package main

import (
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var outputHeader = []string{
	"inspection_id", "inspection_time", "status_synced_at", "district",
	"property_type", "available_slots", "daily_views", "is_completed",
	"sync_age_minutes", "risk_score", "risk_level", "risk_reasons", "recommended_action",
}

// WriteRiskResults 将评分结果写入新的 CSV，不修改原始数据文件。
func WriteRiskResults(path string, inspections []Inspection, model *RiskModel) error {
	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("创建输出目录失败: %w", err)
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write(outputHeader); err != nil {
		return fmt.Errorf("写入输出表头失败: %w", err)
	}

	for _, inspection := range inspections {
		result := model.Score(inspection)
		row := []string{
			inspection.ID,
			inspection.InspectionTime.Format(time.RFC3339),
			inspection.StatusSyncedAt.Format(time.RFC3339),
			inspection.District,
			inspection.PropertyType,
			strconv.Itoa(inspection.AvailableSlots),
			strconv.Itoa(inspection.DailyViews),
			completedText(inspection.Completed),
			strconv.Itoa(inspection.SyncAgeMinutes()),
			strconv.FormatFloat(result.Score, 'f', 4, 64),
			result.Level,
			result.Reasons,
			result.Action,
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("写入 inspection_id=%s 失败: %w", inspection.ID, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("保存输出文件失败: %w", err)
	}
	return nil
}

func completedText(completed *bool) string {
	if completed == nil {
		return ""
	}
	if *completed {
		return "1"
	}
	return "0"
}

type BacktestResult struct {
	TestCount     int
	AlertCount    int
	FailureCount  int
	CaughtFailure int
}

func (r BacktestResult) AlertRate() float64 {
	return safeDivide(r.AlertCount, r.TestCount)
}

func (r BacktestResult) FailureRecall() float64 {
	return safeDivide(r.CaughtFailure, r.FailureCount)
}

func (r BacktestResult) AlertPrecision() float64 {
	return safeDivide(r.CaughtFailure, r.AlertCount)
}

// Backtest 使用 inspection_id 做固定的 8:2 切分。
// 固定切分能保证每次运行得到相同结果，方便比较规则调整前后的效果。
func Backtest(inspections []Inspection) (BacktestResult, error) {
	var trainSet []Inspection
	var testSet []Inspection

	for _, inspection := range inspections {
		if inspection.Completed == nil {
			continue
		}
		if stableBucket(inspection.ID) == 0 {
			testSet = append(testSet, inspection)
		} else {
			trainSet = append(trainSet, inspection)
		}
	}

	if len(testSet) == 0 {
		return BacktestResult{}, fmt.Errorf("没有可用于回测的测试样本")
	}
	model, err := TrainRiskModel(trainSet)
	if err != nil {
		return BacktestResult{}, err
	}

	result := BacktestResult{TestCount: len(testSet)}
	for _, inspection := range testSet {
		failed := !*inspection.Completed
		alerted := model.Score(inspection).Score >= mediumRiskThreshold
		if failed {
			result.FailureCount++
		}
		if alerted {
			result.AlertCount++
		}
		if failed && alerted {
			result.CaughtFailure++
		}
	}
	return result, nil
}

func stableBucket(id string) uint32 {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(id))
	return hasher.Sum32() % 5
}

func safeDivide(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
