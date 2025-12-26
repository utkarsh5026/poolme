package runner

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/olekukonko/tablewriter"
)

// RenderResults is called by CPUBenchmark to render two tables (Throughput + Latency)
func (c *CPUBenchmark) RenderResults(results []RunResult) {
	successRes, err := getSuccessfulResults(results)
	if err != nil {
		return
	}
	fastestTime := successRes[0].TotalTime

	printSectionHeader("THROUGHPUT COMPARISON",
		"How many tasks per second each scheduler can process")

	throughputTable := tablewriter.NewWriter(os.Stdout)
	throughputTable.Header("Rank", "Strategy", "Total Time", "Tasks/sec", "vs Fastest")

	for _, r := range successRes {
		_ = throughputTable.Append(
			getRankIcon(r.Rank),
			r.Strategy,
			r.TotalTime.Round(time.Millisecond).String(),
			FormatNumber(int(r.ThroughputRowsPS)),
			getVsFastestStr(r.TotalTime, fastestTime, r.Rank),
		)
	}

	if err := throughputTable.Render(); err != nil {
		colorPrintLn(Red, "Error in rendering thr table")
	}

	// Latency table
	printSectionHeader("⚡ LATENCY COMPARISON",
		"How long individual tasks take to process (lower is better)",
		"  • P50: 50% of tasks complete within this time",
		"  • P95: 95% of tasks complete within this time",
		"  • P99: 99% of tasks complete within this time")

	latencyTable := tablewriter.NewWriter(os.Stdout)
	latencyTable.Header("Rank", "Strategy", "P50 (median)", "P95", "P99")

	for _, r := range successRes {
		_ = latencyTable.Append(
			getRankIcon(r.Rank),
			r.Strategy,
			FormatLatency(r.P50Latency),
			FormatLatency(r.P95Latency),
			FormatLatency(r.P99Latency),
		)
	}

	if err := latencyTable.Render(); err != nil {
		colorPrintLn(Red, "Error in rendering latency table")
	}

	printFooter(results, successRes)
}

// RenderResults is called by IOBenchmark to render one table
func (i *IOBenchmark) RenderResults(results []RunResult) {
	successfulResults, err := getSuccessfulResults(results)
	if err != nil {
		return
	}
	fastestTime := successfulResults[0].TotalTime

	printSectionHeader("I/O BENCHMARK RESULTS (Isolated Strategy Runs)")

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Strategy", "Time", "Tasks/sec", "Avg Latency", "P95", "P99", "vs Fastest")

	for _, r := range successfulResults {
		_ = table.Append(
			getRankIcon(r.Rank),
			r.Strategy,
			r.TotalTime.Round(time.Millisecond).String(),
			FormatNumber(int(r.ThroughputRowsPS)),
			r.AvgLatency.Round(time.Microsecond).String(),
			r.P95Latency.Round(time.Microsecond).String(),
			r.P99Latency.Round(time.Microsecond).String(),
			getVsFastestStr(r.TotalTime, fastestTime, r.Rank),
		)
	}

	_ = table.Render()
	printFooter(results, successfulResults)
}

// RenderResults is called by PipelineBenchmark to render one table
func (p *PipelineBenchmark) RenderResults(results []RunResult) {
	successfulResults, err := getSuccessfulResults(results)
	if err != nil {
		return
	}
	fastestTime := successfulResults[0].TotalTime

	printSectionHeader("PIPELINE BENCHMARK RESULTS (Isolated Strategy Runs)")

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Strategy", "Time", "Tasks/sec", "MB/sec", "Avg Latency", "P95", "P99", "vs Fastest")

	for _, r := range successfulResults {
		_ = table.Append(
			getRankIcon(r.Rank),
			r.Strategy,
			r.TotalTime.Round(time.Millisecond).String(),
			FormatNumber(int(r.ThroughputRowsPS)),
			fmt.Sprintf("%.1f", r.ThroughputMBPS),
			r.AvgLatency.Round(time.Microsecond).String(),
			r.P95Latency.Round(time.Microsecond).String(),
			r.P99Latency.Round(time.Microsecond).String(),
			getVsFastestStr(r.TotalTime, fastestTime, r.Rank),
		)
	}

	_ = table.Render()
	printFooter(results, successfulResults)
}

func getRankIcon(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return fmt.Sprintf("%d", rank)
	}
}

// getVsFastestStr calculates and formats the "vs Fastest" comparison string
func getVsFastestStr(totalTime, fastestTime time.Duration, rank int) string {
	if rank == 1 {
		return "baseline"
	}
	vsFastest := float64(totalTime) / float64(fastestTime)
	return fmt.Sprintf("%.2fx", vsFastest)
}

func printSectionHeader(title string, descriptions ...string) {
	fmt.Println()
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	colorPrintLn(Bold, title)
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	for _, desc := range descriptions {
		fmt.Println(desc)
	}
	fmt.Println()
}

func printFooter(results []RunResult, successfulResults []RunResult) {
	printFailedStrategies(results)
	fmt.Println()
	colorPrintf(Green, "✅ Successfully tested %d/%d strategies\n", len(successfulResults), len(results))
	fmt.Println()
}

// printFailedStrategies is a shared helper to print failed strategies
func printFailedStrategies(results []RunResult) {
	failedResults := make([]RunResult, 0)
	for _, r := range results {
		if !r.Success {
			failedResults = append(failedResults, r)
		}
	}

	if len(failedResults) > 0 {
		fmt.Println()
		colorPrintLn(Red, "⚠️  Failed Strategies:")
		for _, r := range failedResults {
			colorPrintf(Red, "  • %s: %s\n", r.Strategy, r.ErrorMsg)
		}
	}
}

func getSuccessfulResults(results []RunResult) ([]RunResult, error) {
	successfulResults := make([]RunResult, 0)
	for _, r := range results {
		if r.Success {
			successfulResults = append(successfulResults, r)
		}
	}

	if len(successfulResults) == 0 {
		colorPrintLn(Red, "No strategies completed successfully!")
		return nil, fmt.Errorf("no strategies completed successfully")
	}

	sort.Slice(successfulResults, func(i, j int) bool {
		return successfulResults[i].TotalTime < successfulResults[j].TotalTime
	})

	for i := range successfulResults {
		successfulResults[i].Rank = i + 1
	}
	return successfulResults, nil
}
