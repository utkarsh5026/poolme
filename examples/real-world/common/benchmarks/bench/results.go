package bench

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

// ResultsRenderer handles result display with benchmark-specific customization.
type ResultsRenderer struct {
	Title          string
	ShowMBPS       bool // Show MB/sec column (for pipeline)
	ShowAvgLatency bool // Show average latency column (for IO/pipeline)
	ShowP50        bool // Show P50 in table (for CPU separate latency table)
	ShowVsFastest  bool // Show vs Fastest column
}

// PrintComparisonTable displays the performance comparison table.
// Results are sorted by total time and ranked before being displayed.
func (rr *ResultsRenderer) PrintComparisonTable(results []runner.StrategyResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	for i := range results {
		results[i].Rank = i + 1
	}

	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println(fmt.Sprintf("📊 %s", rr.Title))
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	table := tablewriter.NewWriter(os.Stdout)

	headers := []string{"Rank", "Scheduler", "Total Time", "Tasks/sec"}
	if rr.ShowMBPS {
		headers = append(headers, "MB/sec")
	}
	if rr.ShowAvgLatency {
		headers = append(headers, "Avg Latency")
	}
	headers = append(headers, "P95", "P99")
	if rr.ShowVsFastest {
		headers = append(headers, "vs Fastest")
	}

	headerAny := make([]any, len(headers))
	for i, h := range headers {
		headerAny[i] = h
	}
	table.Header(headerAny...)

	fastestTime := results[0].TotalTime.Seconds()
	for _, r := range results {
		row := []string{
			RankIcon(r.Rank),
			r.Name,
			r.TotalTime.Round(time.Millisecond).String(),
			runner.FormatNumber(int(r.ThroughputRowsPS)),
		}

		if rr.ShowMBPS {
			row = append(row, fmt.Sprintf("%.2f", r.ThroughputMBPS))
		}

		if rr.ShowAvgLatency {
			row = append(row, runner.FormatLatency(r.AvgLatency))
		}

		row = append(row,
			runner.FormatLatency(r.P95Latency),
			runner.FormatLatency(r.P99Latency),
		)

		if rr.ShowVsFastest {
			var comparison string
			if r.Rank == 1 {
				comparison = "baseline"
			} else {
				pct := ((r.TotalTime.Seconds() / fastestTime) - 1) * 100
				comparison = fmt.Sprintf("+%.1f%%", pct)
			}
			row = append(row, comparison)
		}

		_ = table.Append(row)
	}

	_ = table.Render()
	fmt.Println()
}

// RankIcon returns the appropriate icon for a rank.
func RankIcon(rank int) string {
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
