package runner

import (
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"time"
)

// StrategyResult holds the results from running a single strategy
// This is a superset type used by all benchmarks
type StrategyResult struct {
	Name                                           string
	TotalTime                                      time.Duration
	Rank                                           int
	ThroughputRowsPS, ThroughputMBPS               float64
	AvgLatency, P50Latency, P95Latency, P99Latency time.Duration
}

// DefaultCalculateStats calculates median stats (simple strategy)
// Used by I/O and Pipeline benchmarks
func DefaultCalculateStats(strategyName string, results []StrategyResult) StrategyResult {
	if len(results) == 0 {
		return StrategyResult{Name: strategyName}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	medianIdx := len(results) / 2
	median := results[medianIdx]
	median.Name = strategyName

	return median
}

// CalculateStatsWithLatencyAveraging calculates stats with percentile averaging
// Used by CPU benchmark for more accurate latency metrics
func CalculateStatsWithLatencyAveraging(strategyName string, results []StrategyResult) StrategyResult {
	if len(results) == 0 {
		return StrategyResult{Name: strategyName}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	medianIdx := len(results) / 2
	median := results[medianIdx]

	var avgP50, avgP95, avgP99 time.Duration
	for _, r := range results {
		avgP50 += r.P50Latency
		avgP95 += r.P95Latency
		avgP99 += r.P99Latency
	}
	n := time.Duration(len(results))
	avgP50 /= n
	avgP95 /= n
	avgP99 /= n

	return StrategyResult{
		Name:             strategyName,
		TotalTime:        median.TotalTime,
		ThroughputMBPS:   median.ThroughputMBPS,
		ThroughputRowsPS: median.ThroughputRowsPS,
		P50Latency:       avgP50,
		P95Latency:       avgP95,
		P99Latency:       avgP99,
	}
}

// PrintIterationStats prints detailed statistics for multiple iterations
func PrintIterationStats(results []StrategyResult) {
	if len(results) <= 1 {
		return
	}

	times := make([]time.Duration, len(results))
	for i, r := range results {
		times[i] = r.TotalTime
	}

	slices.Sort(times)

	mini := times[0]
	maxi := times[len(times)-1]
	median := times[len(times)/2]

	var sum time.Duration
	for _, t := range times {
		sum += t
	}
	mean := sum / time.Duration(len(times))
	var variance float64
	for _, t := range times {
		diff := float64(t - mean)
		variance += diff * diff
	}
	stddev := time.Duration(math.Sqrt(variance / float64(len(times))))

	fmt.Printf("    Min: %v | Median: %v | Mean: %v | Max: %v | StdDev: %v\n",
		mini.Round(time.Millisecond),
		median.Round(time.Millisecond),
		mean.Round(time.Millisecond),
		maxi.Round(time.Millisecond),
		stddev.Round(time.Millisecond))
}

// GetStrategiesToRun determines which strategies to run based on flags
func GetStrategiesToRun(allStrategies []string, isolated string, iterations int, warmup int) []string {
	if isolated != "" {
		found := slices.Contains(allStrategies, isolated)
		if !found {
			colorPrintf(Red, "Error: Unknown strategy '%s'\n", isolated)
			fmt.Println("Available strategies:", allStrategies)
			os.Exit(1)
		}
		fmt.Printf("🔬 SINGLE STRATEGY MODE: Testing '%s' scheduler\n", isolated)
		if iterations > 1 {
			fmt.Printf("  Running %d iterations with %d warmup runs\n", iterations, warmup)
		}
		fmt.Println()
		return []string{isolated}
	}
	return allStrategies
}
