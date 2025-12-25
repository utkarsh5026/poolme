package cpu

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

var (
	bold = color.New(color.Bold)
)

func printConfiguration(numWorkers int, numTasks int, workload string) {
	complexity := *complexityFlag

	_, _ = bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:    %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Tasks:      %s concurrent tasks\n", runner.FormatNumber(numTasks))
	fmt.Printf("  Complexity: %s iterations per task\n", runner.FormatNumber(complexity))
	fmt.Printf("  Workload:   %s\n", workload)
	fmt.Printf("  Strategies: 7 schedulers\n")
	fmt.Println()

	_, _ = bold.Println("📊 Workload Distribution:")
	switch workload {
	case "balanced":
		fmt.Printf("  • All %s tasks have equal complexity (%s iterations)\n",
			runner.FormatNumber(numTasks), runner.FormatNumber(complexity))
	case "imbalanced":
		fmt.Printf("  • 10%% Heavy tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*10/100), runner.FormatNumber(complexity*10))
		fmt.Printf("  • 20%% Medium tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*20/100), runner.FormatNumber(complexity*5))
		fmt.Printf("  • 70%% Light tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*70/100), runner.FormatNumber(complexity))
	case "priority":
		fmt.Printf("  • Imbalanced workload submitted in reversed order (light → heavy)\n")
		fmt.Printf("  • Tests if priority schedulers reorder tasks effectively\n")
	}
	fmt.Println()
}

func printResults(results []runner.StrategyResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	for i := range results {
		results[i].Rank = i + 1
	}

	printComparisonTable(results)
}

func printComparisonTable(results []runner.StrategyResult) {
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("📊 THROUGHPUT COMPARISON")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Scheduler", "Total Time", "Tasks/sec", "vs Fastest")

	fastestTime := results[0].TotalTime.Seconds()

	for _, r := range results {
		rankIcon := fmt.Sprintf("%d", r.Rank)
		switch r.Rank {
		case 1:
			rankIcon = "🥇"
		case 2:
			rankIcon = "🥈"
		case 3:
			rankIcon = "🥉"
		}

		timeStr := r.TotalTime.Round(time.Millisecond).String()
		throughputStr := runner.FormatNumber(int(r.ThroughputRowsPS))

		var comparison string
		if r.Rank == 1 {
			comparison = "baseline"
		} else {
			pct := ((r.TotalTime.Seconds() / fastestTime) - 1) * 100
			comparison = fmt.Sprintf("+%.1f%%", pct)
		}

		_ = table.Append(rankIcon, r.Name, timeStr, throughputStr, comparison)
	}

	_ = table.Render()
	fmt.Println()
	fmt.Println()

	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("⚡ LATENCY COMPARISON")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	latencyTable := tablewriter.NewWriter(os.Stdout)
	latencyTable.Header("Rank", "Scheduler", "P50 (median)", "P95", "P99")

	for _, r := range results {
		rankIcon := fmt.Sprintf("%d", r.Rank)
		switch r.Rank {
		case 1:
			rankIcon = "🥇"
		case 2:
			rankIcon = "🥈"
		case 3:
			rankIcon = "🥉"
		}

		_ = latencyTable.Append(
			rankIcon,
			r.Name,
			runner.FormatLatency(r.P50Latency),
			runner.FormatLatency(r.P95Latency),
			runner.FormatLatency(r.P99Latency),
		)
	}

	_ = latencyTable.Render()
	fmt.Println()
}
