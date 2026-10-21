package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	inputPath := flag.String("input", "inspections.csv", "输入 CSV 文件路径")
	outputPath := flag.String("output", "risk_results.csv", "输出 CSV 文件路径")
	scoreAll := flag.Bool("score-all", false, "是否给全部记录评分；默认只处理结果为空的记录")
	flag.Parse()

	if err := run(*inputPath, *outputPath, *scoreAll); err != nil {
		fmt.Fprintln(os.Stderr, "运行失败:", err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string, scoreAll bool) error {
	inspections, duplicateCount, err := ReadInspections(inputPath)
	if err != nil {
		return err
	}

	model, err := TrainRiskModel(inspections)
	if err != nil {
		return err
	}

	// 先用留出数据检查规则是否真的能拦截历史失败带看。
	backtest, err := Backtest(inspections)
	if err != nil {
		return fmt.Errorf("回测失败: %w", err)
	}

	targets := selectTargets(inspections, scoreAll)
	if len(targets) == 0 {
		return fmt.Errorf("没有需要评分的记录，可使用 -score-all 给全部记录评分")
	}
	if err := WriteRiskResults(outputPath, targets, model); err != nil {
		return err
	}

	fmt.Printf("读取记录: %d 条（去除完全重复记录 %d 条）\n", len(inspections), duplicateCount)
	fmt.Printf("历史整体未完成率: %.2f%%\n", model.FailureRate()*100)
	fmt.Printf("回测样本: %d 条，需干预 %.2f%%，失败召回率 %.2f%%，干预命中率 %.2f%%\n",
		backtest.TestCount, backtest.AlertRate()*100,
		backtest.FailureRecall()*100, backtest.AlertPrecision()*100)
	fmt.Printf("已输出评分: %d 条 -> %s\n", len(targets), outputPath)
	return nil
}

func selectTargets(inspections []Inspection, scoreAll bool) []Inspection {
	if scoreAll {
		return inspections
	}
	targets := make([]Inspection, 0)
	for _, inspection := range inspections {
		if inspection.Completed == nil {
			targets = append(targets, inspection)
		}
	}
	return targets
}
