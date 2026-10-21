package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

var requiredColumns = []string{
	"inspection_id", "inspection_time", "status_synced_at", "district",
	"property_type", "available_slots", "daily_views", "is_completed",
}

// Inspection 表示 CSV 中的一条带看记录。
// Completed 为 nil 时，说明这条记录还没有最终结果，需要进行风险评分。
type Inspection struct {
	ID             string
	InspectionTime time.Time
	StatusSyncedAt time.Time
	District       string
	PropertyType   string
	AvailableSlots int
	DailyViews     int
	Completed      *bool
}

// SyncAgeMinutes 返回房源状态距离带看时间已经过去了多少分钟。
func (i Inspection) SyncAgeMinutes() int {
	return int(i.InspectionTime.Sub(i.StatusSyncedAt) / time.Minute)
}

// ReadInspections 读取并校验 CSV，同时去掉 inspection_id 完全相同的重复行。
func ReadInspections(path string) ([]Inspection, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("打开 CSV 失败: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, 0, fmt.Errorf("读取表头失败: %w", err)
	}

	columns, err := buildColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}

	var inspections []Inspection
	seen := make(map[string]Inspection)
	duplicateCount := 0

	for lineNumber := 2; ; lineNumber++ {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("第 %d 行读取失败: %w", lineNumber, readErr)
		}

		inspection, parseErr := parseInspection(row, columns)
		if parseErr != nil {
			return nil, 0, fmt.Errorf("第 %d 行数据错误: %w", lineNumber, parseErr)
		}

		if previous, exists := seen[inspection.ID]; exists {
			if !sameInspection(previous, inspection) {
				return nil, 0, fmt.Errorf("第 %d 行的 inspection_id=%s 与前面记录冲突", lineNumber, inspection.ID)
			}
			duplicateCount++
			continue
		}

		seen[inspection.ID] = inspection
		inspections = append(inspections, inspection)
	}

	if len(inspections) == 0 {
		return nil, 0, errors.New("CSV 中没有数据")
	}
	return inspections, duplicateCount, nil
}

func buildColumnIndex(header []string) (map[string]int, error) {
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	for _, name := range requiredColumns {
		if _, exists := columns[name]; !exists {
			return nil, fmt.Errorf("CSV 缺少必要字段 %q", name)
		}
	}
	return columns, nil
}

func parseInspection(row []string, columns map[string]int) (Inspection, error) {
	value := func(name string) string {
		index := columns[name]
		if index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}

	id := value("inspection_id")
	if id == "" {
		return Inspection{}, errors.New("inspection_id 不能为空")
	}

	inspectionTime, err := parseTimeField(value("inspection_time"), "inspection_time")
	if err != nil {
		return Inspection{}, err
	}
	statusSyncedAt, err := parseTimeField(value("status_synced_at"), "status_synced_at")
	if err != nil {
		return Inspection{}, err
	}
	if statusSyncedAt.After(inspectionTime) {
		return Inspection{}, errors.New("status_synced_at 不能晚于 inspection_time")
	}

	district := value("district")
	propertyType := value("property_type")
	if district == "" || propertyType == "" {
		return Inspection{}, errors.New("district 和 property_type 不能为空")
	}

	availableSlots, err := parseNonNegativeInt(value("available_slots"), "available_slots")
	if err != nil {
		return Inspection{}, err
	}
	dailyViews, err := parseNonNegativeInt(value("daily_views"), "daily_views")
	if err != nil {
		return Inspection{}, err
	}
	completed, err := parseCompleted(value("is_completed"))
	if err != nil {
		return Inspection{}, err
	}

	return Inspection{
		ID: id, InspectionTime: inspectionTime, StatusSyncedAt: statusSyncedAt,
		District: district, PropertyType: propertyType, AvailableSlots: availableSlots,
		DailyViews: dailyViews, Completed: completed,
	}, nil
}

func parseTimeField(text, name string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s 不是合法的 RFC3339 时间: %q", name, text)
	}
	return parsed, nil
}

func parseNonNegativeInt(text, name string) (int, error) {
	parsed, err := strconv.Atoi(text)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s 必须是非负整数: %q", name, text)
	}
	return parsed, nil
}

func parseCompleted(text string) (*bool, error) {
	switch text {
	case "":
		return nil, nil
	case "0":
		value := false
		return &value, nil
	case "1":
		value := true
		return &value, nil
	default:
		return nil, fmt.Errorf("is_completed 只能是 0、1 或空值: %q", text)
	}
}

func sameInspection(left, right Inspection) bool {
	return left.ID == right.ID && left.InspectionTime.Equal(right.InspectionTime) &&
		left.StatusSyncedAt.Equal(right.StatusSyncedAt) && left.District == right.District &&
		left.PropertyType == right.PropertyType && left.AvailableSlots == right.AvailableSlots &&
		left.DailyViews == right.DailyViews && sameCompleted(left.Completed, right.Completed)
}

func sameCompleted(left, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
