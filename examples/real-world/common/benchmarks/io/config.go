package io

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
	latency := *latencyFlag
	_, _ = bold.Println("⚙️  I/O Benchmark Configuration:")
	fmt.Printf("  Workers:    %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Tasks:      %s concurrent I/O operations\n", runner.FormatNumber(numTasks))
	fmt.Printf("  Workload:   %s\n", workload)
	fmt.Printf("  Avg Latency: %dms per operation\n", latency)
	fmt.Printf("  Strategies: 7 schedulers\n")
	fmt.Println()

	_, _ = bold.Println("📊 Workload Distribution:")
	switch workload {
	case "api":
		fmt.Printf("  • Simulates API Gateway: 80%% normal, 15%% fast, 5%% slow requests\n")
		fmt.Printf("  • Network I/O with ±30%% latency variance\n")
	case "database":
		fmt.Printf("  • Simulates Database Queries: 70%% fast, 25%% medium, 5%% slow (N+1)\n")
		fmt.Printf("  • Query I/O with ±15%% latency variance\n")
	case "file":
		fmt.Printf("  • Simulates File I/O: 50%% cache hits, 40%% disk reads, 10%% slow seeks\n")
		fmt.Printf("  • Disk I/O with ±40%% latency variance\n")
	case "mixed":
		fmt.Printf("  • Mixed I/O + CPU: 60%% CPU-light, 30%% balanced, 10%% CPU-heavy\n")
		fmt.Printf("  • Combines network I/O with computational work\n")
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
	printIOOperationsSummary()
}

func printComparisonTable(results []runner.StrategyResult) {
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("📊 I/O SCHEDULER PERFORMANCE")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Scheduler", "Time", "Tasks/sec", "Avg Latency", "P95", "P99", "vs Fastest")

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
		avgLatencyStr := runner.FormatLatency(r.AvgLatency)
		p95Str := runner.FormatLatency(r.P95Latency)
		p99Str := runner.FormatLatency(r.P99Latency)

		var comparison string
		if r.Rank == 1 {
			comparison = "baseline"
		} else {
			pct := ((r.TotalTime.Seconds() / fastestTime) - 1) * 100
			comparison = fmt.Sprintf("+%.1f%%", pct)
		}

		_ = table.Append(rankIcon, r.Name, timeStr, throughputStr, avgLatencyStr, p95Str, p99Str, comparison)
	}

	_ = table.Render()
	fmt.Println()
}

func printIOOperationsSummary() {
	apiCalls := totalAPICalls.Load()
	dbQueries := totalDBQueries.Load()
	fileOps := totalFileOps.Load()

	if apiCalls > 0 || dbQueries > 0 || fileOps > 0 {
		_, _ = bold.Println("📈 I/O Operations Summary:")
		if apiCalls > 0 {
			fmt.Printf("  API Calls:       %s\n", runner.FormatNumber(int(apiCalls)))
		}
		if dbQueries > 0 {
			fmt.Printf("  DB Queries:      %s\n", runner.FormatNumber(int(dbQueries)))
		}
		if fileOps > 0 {
			fmt.Printf("  File Operations: %s\n", runner.FormatNumber(int(fileOps)))
		}
		fmt.Println()
	}
}
