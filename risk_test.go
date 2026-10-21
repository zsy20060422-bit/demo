package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncAgeMinutesHandlesTimezone(t *testing.T) {
	inspection := Inspection{
		InspectionTime: mustParseTime(t, "2026-09-01T07:01:00+08:00"),
		StatusSyncedAt: mustParseTime(t, "2026-08-31T22:09:00Z"),
	}

	if got, want := inspection.SyncAgeMinutes(), 52; got != want {
		t.Fatalf("SyncAgeMinutes() = %d, want %d", got, want)
	}
}

func TestRiskModelGivesStaleRecordHigherRisk(t *testing.T) {
	completed := true
	failed := false
	baseTime := mustParseTime(t, "2026-09-01T10:00:00+08:00")

	var history []Inspection
	for index := 0; index < 40; index++ {
		result := &completed
		age := 5
		if index >= 30 {
			result = &failed
			age = 65
		}
		history = append(history, Inspection{
			ID: string(rune('a' + index)), InspectionTime: baseTime,
			StatusSyncedAt: baseTime.Add(-time.Duration(age) * time.Minute),
			District:       "北部大区", PropertyType: "标准两居",
			AvailableSlots: 10, DailyViews: 50, Completed: result,
		})
	}

	model, err := TrainRiskModel(history)
	if err != nil {
		t.Fatal(err)
	}
	fresh := history[0]
	stale := history[0]
	stale.StatusSyncedAt = stale.InspectionTime.Add(-65 * time.Minute)
	if model.Score(stale).Score <= model.Score(fresh).Score {
		t.Fatal("状态同步更旧的记录应该得到更高风险")
	}
}

func TestReadInspectionsRemovesExactDuplicates(t *testing.T) {
	content := strings.Join([]string{
		"inspection_id,inspection_time,status_synced_at,district,property_type,available_slots,daily_views,is_completed",
		"1001,2026-09-01T10:00:00+08:00,2026-09-01T01:55:00Z,北部大区,标准两居,10,50,1",
		"1001,2026-09-01T10:00:00+08:00,2026-09-01T01:55:00Z,北部大区,标准两居,10,50,1",
	}, "\n")

	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	records, duplicates, err := ReadInspections(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || duplicates != 1 {
		t.Fatalf("records=%d duplicates=%d, want records=1 duplicates=1", len(records), duplicates)
	}
}

func TestRiskThresholds(t *testing.T) {
	tests := []struct {
		risk      float64
		wantLevel string
	}{
		{risk: 0.19, wantLevel: "低风险"},
		{risk: 0.20, wantLevel: "中风险"},
		{risk: 0.35, wantLevel: "高风险"},
	}

	for _, test := range tests {
		level, _ := riskLevelAndAction(test.risk)
		if level != test.wantLevel {
			t.Errorf("riskLevelAndAction(%.2f) = %s, want %s", test.risk, level, test.wantLevel)
		}
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
