package main

import (
	"fmt"
	"math"
)

const (
	// 平滑强度相当于为每个小分组补充 20 条接近整体水平的虚拟样本，
	// 避免样本很少的分组出现 0% 或 100% 这类不稳定结果。
	priorStrength = 20.0

	mediumRiskThreshold = 0.20
	highRiskThreshold   = 0.35
)

// rateStat 保存一个分组中的总数和未完成数。
type rateStat struct {
	Total    int
	Failures int
}

func (s *rateStat) add(completed bool) {
	s.Total++
	if !completed {
		s.Failures++
	}
}

// RiskModel 是一个可解释的分组风险模型。
// 状态同步时效是主要信号，其他字段只做辅助修正。
type RiskModel struct {
	overall    rateStat
	bySyncAge  map[string]*rateStat
	bySlots    map[string]*rateStat
	byViews    map[string]*rateStat
	byDistrict map[string]*rateStat
	byProperty map[string]*rateStat
}

// RiskResult 是给业务侧使用的评分结果。
type RiskResult struct {
	Score   float64
	Level   string
	Reasons string
	Action  string
}

func TrainRiskModel(inspections []Inspection) (*RiskModel, error) {
	model := &RiskModel{
		bySyncAge: make(map[string]*rateStat), bySlots: make(map[string]*rateStat),
		byViews: make(map[string]*rateStat), byDistrict: make(map[string]*rateStat),
		byProperty: make(map[string]*rateStat),
	}

	for _, inspection := range inspections {
		if inspection.Completed == nil {
			continue
		}
		completed := *inspection.Completed
		model.overall.add(completed)
		statFor(model.bySyncAge, syncAgeBand(inspection.SyncAgeMinutes())).add(completed)
		statFor(model.bySlots, slotsBand(inspection.AvailableSlots)).add(completed)
		statFor(model.byViews, viewsBand(inspection.DailyViews)).add(completed)
		statFor(model.byDistrict, inspection.District).add(completed)
		statFor(model.byProperty, inspection.PropertyType).add(completed)
	}

	if model.overall.Total < 20 {
		return nil, fmt.Errorf("有效历史样本只有 %d 条，至少需要 20 条", model.overall.Total)
	}
	return model, nil
}

func (m *RiskModel) FailureRate() float64 {
	return float64(m.overall.Failures) / float64(m.overall.Total)
}

// Score 计算未完成风险。权重越高，说明该特征对最终风险的影响越大。
func (m *RiskModel) Score(inspection Inspection) RiskResult {
	syncRate := m.smoothedRate(m.bySyncAge[syncAgeBand(inspection.SyncAgeMinutes())])
	slotsRate := m.smoothedRate(m.bySlots[slotsBand(inspection.AvailableSlots)])
	viewsRate := m.smoothedRate(m.byViews[viewsBand(inspection.DailyViews)])
	districtRate := m.smoothedRate(m.byDistrict[inspection.District])
	propertyRate := m.smoothedRate(m.byProperty[inspection.PropertyType])

	// 状态时效占 60%，其余四项合计占 40%。
	risk := syncRate*0.60 + slotsRate*0.15 + viewsRate*0.15 + districtRate*0.05 + propertyRate*0.05
	risk = math.Max(0, math.Min(1, risk))

	level, action := riskLevelAndAction(risk)
	return RiskResult{
		Score: risk, Level: level,
		Reasons: riskReasons(inspection, m.FailureRate()), Action: action,
	}
}

func (m *RiskModel) smoothedRate(stat *rateStat) float64 {
	overallRate := m.FailureRate()
	if stat == nil || stat.Total == 0 {
		return overallRate
	}
	return (float64(stat.Failures) + overallRate*priorStrength) /
		(float64(stat.Total) + priorStrength)
}

func statFor(group map[string]*rateStat, key string) *rateStat {
	stat, exists := group[key]
	if !exists {
		stat = &rateStat{}
		group[key] = stat
	}
	return stat
}

func syncAgeBand(minutes int) string {
	switch {
	case minutes <= 15:
		return "0-15"
	case minutes <= 30:
		return "16-30"
	case minutes <= 60:
		return "31-60"
	default:
		return "61+"
	}
}

func slotsBand(slots int) string {
	switch {
	case slots <= 3:
		return "0-3"
	case slots <= 7:
		return "4-7"
	case slots <= 15:
		return "8-15"
	case slots <= 25:
		return "16-25"
	default:
		return "26+"
	}
}

func viewsBand(views int) string {
	switch {
	case views <= 20:
		return "0-20"
	case views <= 40:
		return "21-40"
	case views <= 60:
		return "41-60"
	case views <= 80:
		return "61-80"
	default:
		return "81+"
	}
}

func riskLevelAndAction(risk float64) (string, string) {
	switch {
	case risk >= highRiskThreshold:
		return "高风险", "暂停带看，先由房源维护人确认房源仍可交易"
	case risk >= mediumRiskThreshold:
		return "中风险", "立即刷新房源状态，并请经纪人在出发前确认"
	default:
		return "低风险", "正常提醒，并在带看前再次自动校验"
	}
}

func riskReasons(inspection Inspection, overallRate float64) string {
	reasons := make([]string, 0, 3)
	if inspection.SyncAgeMinutes() > 15 {
		reasons = append(reasons, fmt.Sprintf("房源状态已 %d 分钟未同步", inspection.SyncAgeMinutes()))
	}
	if inspection.AvailableSlots <= 3 {
		reasons = append(reasons, "可预约时段较少")
	}
	if inspection.DailyViews > 80 {
		reasons = append(reasons, "日浏览量较高，房源竞争较激烈")
	}
	if len(reasons) == 0 {
		return fmt.Sprintf("未出现明显风险信号，历史整体未完成率约 %.1f%%", overallRate*100)
	}

	result := reasons[0]
	for index := 1; index < len(reasons); index++ {
		result += "；" + reasons[index]
	}
	return result
}
