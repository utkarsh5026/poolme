package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"slices"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/utkarsh5026/poolme/pool"
)

var (
	allStrategies = []string{
		"Channel",
		"Work-Stealing",
		"MPMC Queue",
		"LMAX Disruptor",
		"Priority Queue",
		"Skip List",
		"Bitmask",
	}
)

// StationData represents a weather station's measurement batch (or chunk)
type StationData struct {
	StationName string
	ChunkID     int     // Chunk number for this station
	NumReadings int     // Number of readings in this chunk
	SizeMB      float64 // Size in MB for this chunk
	Complexity  string  // Tier: Huge, Large, Medium, Small, Tiny
	StartIndex  int     // Starting index for seed offset
}

// StationStats represents aggregated statistics for a station
type StationStats struct {
	StationName string
	NumReadings int
	MinTemp     float64
	MaxTemp     float64
	AvgTemp     float64
	ProcessTime time.Duration
}

// StrategyResult holds the results for a strategy
type StrategyResult struct {
	Name             string
	TotalTime        time.Duration
	ThroughputMBps   float64
	ThroughputRowsPS float64
	Rank             int
}

var (
	bold = color.New(color.Bold)
	red  = color.New(color.FgRed)
)

func processStation(ctx context.Context, station StationData) (StationStats, error) {
	start := time.Now()

	minTemp := 100.0
	maxTemp := -100.0
	sumTemp := 0.0

	seed := int64(hashString(station.StationName)) + int64(station.StartIndex)
	rng := rand.New(rand.NewSource(seed))

	for i := 0; i < station.NumReadings; i++ {
		temp := rng.Float64()*60 - 20
		temp = math.Round(temp*10) / 10

		if temp < minTemp {
			minTemp = temp
		}
		if temp > maxTemp {
			maxTemp = temp
		}
		sumTemp += temp

		if i%1000 == 0 {
			_ = math.Sin(float64(i)) * math.Cos(temp)
		}
	}

	avgTemp := sumTemp / float64(station.NumReadings)

	return StationStats{
		StationName: station.StationName,
		NumReadings: station.NumReadings,
		MinTemp:     math.Round(minTemp*10) / 10,
		MaxTemp:     math.Round(maxTemp*10) / 10,
		AvgTemp:     math.Round(avgTemp*10) / 10,
		ProcessTime: time.Since(start),
	}, nil
}

func hashString(s string) int64 {
	hash := int64(0)
	for _, c := range s {
		hash = hash*31 + int64(c)
	}
	return hash
}

// generateBalancedStationData creates uniform-sized chunks for all stations.
// This creates a perfectly balanced workload where all tasks have the same size,
// reducing the impact of workload imbalance on scheduler performance.
//
// chunkSize: Rows per task (all tasks will have this size except possibly the last)
func generateBalancedStationData(totalRows int, chunkSize int) []StationData {
	tasks := make([]StationData, 0)
	stationNames := []string{
		"Beijing", "Tokyo", "Delhi", "Shanghai", "Mumbai", "Cairo", "Moscow",
		"London", "Paris", "Berlin", "Rome", "Madrid", "Amsterdam", "Brussels",
		"Vienna", "Oslo", "Helsinki", "Stockholm", "Copenhagen", "Dublin",
		"Lisbon", "Athens", "Warsaw", "Prague", "Budapest", "Reykjavik",
		"Luxembourg", "Monaco", "Vaduz", "Andorra",
	}

	numChunks := (totalRows + chunkSize - 1) / chunkSize
	stationIdx := 0

	for chunkID := range numChunks {
		startIdx := chunkID * chunkSize
		endIdx := min((chunkID+1)*chunkSize, totalRows)
		chunkRows := endIdx - startIdx

		if chunkRows <= 0 {
			continue
		}

		// Round-robin station assignment for perfect distribution
		stationName := stationNames[stationIdx%len(stationNames)]
		stationIdx++

		tasks = append(tasks, StationData{
			StationName: stationName,
			ChunkID:     chunkID,
			NumReadings: chunkRows,
			SizeMB:      float64(chunkRows) * 0.000026, // ~26 bytes per row
			Complexity:  "Uniform",
			StartIndex:  startIdx,
		})
	}

	return tasks
}

// generateStationData creates chunked station data for true parallel testing.
// Instead of 30 large tasks, this creates many smaller chunks to stress-test schedulers.
//
// Distribution maintains same percentages:
// - HUGE stations: 68% of total data (3 stations)
// - Large stations: 27% of total data (4 stations)
// - Medium stations: 4% of total data (8 stations)
// - Small/Tiny stations: 1% of total data (15 stations)
//
// chunkSize: Max rows per task (e.g., 50000 = 50K rows per task)
func generateStationData(totalRows int, chunkSize int) []StationData {
	stationDistribution := []struct {
		name    string
		percent float64
		sizeMB  float64
		tier    string
	}{
		// HUGE stations - 68% total
		{"Beijing", 22.77, 600, "Huge"},
		{"Tokyo", 18.22, 480, "Huge"},
		{"Delhi", 27.32, 720, "Huge"},

		// Large stations - 27% total
		{"Shanghai", 7.59, 200, "Large"},
		{"Mumbai", 6.83, 180, "Large"},
		{"Cairo", 5.77, 152, "Large"},
		{"Moscow", 6.38, 168, "Large"},

		// Medium stations - 4% total
		{"London", 0.68, 18, "Medium"},
		{"Paris", 0.58, 15, "Medium"},
		{"Berlin", 0.64, 17, "Medium"},
		{"Rome", 0.53, 14, "Medium"},
		{"Madrid", 0.59, 16, "Medium"},
		{"Amsterdam", 0.43, 11, "Medium"},
		{"Brussels", 0.47, 12, "Medium"},
		{"Vienna", 0.52, 14, "Medium"},

		// Small/Tiny stations - 1% total
		{"Oslo", 0.068, 1.8, "Small"},
		{"Helsinki", 0.058, 1.5, "Small"},
		{"Stockholm", 0.064, 1.7, "Small"},
		{"Copenhagen", 0.062, 1.6, "Small"},
		{"Dublin", 0.053, 1.4, "Small"},
		{"Lisbon", 0.059, 1.6, "Small"},
		{"Athens", 0.067, 1.8, "Small"},
		{"Warsaw", 0.061, 1.6, "Small"},
		{"Prague", 0.056, 1.5, "Small"},
		{"Budapest", 0.055, 1.4, "Small"},
		{"Reykjavik", 0.018, 0.5, "Tiny"},
		{"Luxembourg", 0.023, 0.6, "Tiny"},
		{"Monaco", 0.012, 0.3, "Tiny"},
		{"Vaduz", 0.014, 0.4, "Tiny"},
		{"Andorra", 0.017, 0.4, "Tiny"},
	}

	tasks := make([]StationData, 0)

	for _, dist := range stationDistribution {
		stationRows := int(float64(totalRows) * dist.percent / 100.0)
		stationSizeMB := dist.sizeMB * (float64(stationRows) / (float64(totalRows) * dist.percent / 100.0))

		numChunks := (stationRows + chunkSize - 1) / chunkSize //
		if numChunks == 0 {
			numChunks = 1
		}

		for chunkID := 0; chunkID < numChunks; chunkID++ {
			startIdx := chunkID * chunkSize
			endIdx := min((chunkID+1)*chunkSize, stationRows)
			chunkRows := endIdx - startIdx

			if chunkRows <= 0 {
				continue
			}

			tasks = append(tasks, StationData{
				StationName: dist.name,
				ChunkID:     chunkID,
				NumReadings: chunkRows,
				SizeMB:      stationSizeMB * (float64(chunkRows) / float64(stationRows)),
				Complexity:  dist.tier,
				StartIndex:  startIdx,
			})
		}
	}

	return tasks
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

// isCIMode detects if running in CI environment
func isCIMode(ciFlag bool) bool {
	if ciFlag {
		return true
	}

	ciEnvVars := []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "JENKINS_HOME"}
	for _, env := range ciEnvVars {
		value := os.Getenv(env)
		if value == "true" || value == "1" {
			return true
		}
	}
	return false
}

func runStrategy(strategyName string, stations []StationData, numWorkers int, bar *progressbar.ProgressBar) StrategyResult {
	ctx := context.Background()

	var workerPool *pool.WorkerPool[StationData, StationStats]

	switch strategyName {
	case "Work-Stealing":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithPriorityQueue(func(a, b StationData) bool {
				return a.NumReadings > b.NumReadings
			}),
		)
	case "Skip List":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithSkipList(func(a, b StationData) bool {
				return a.NumReadings > b.NumReadings
			}),
		)
	case "Bitmask":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
			pool.WithLmax(),
		)
	default:
		workerPool = pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(numWorkers),
		)
	}

	start := time.Now()

	// Process all tasks using the batch Process method
	// This handles submission and result collection internally, avoiding deadlocks
	_, err := workerPool.Process(ctx, stations, processStation)
	if err != nil {
		red.Printf("Error processing %s: %v\n", strategyName, err)
		return StrategyResult{Name: strategyName}
	}

	elapsed := time.Since(start)

	// Update progress bar
	if bar != nil {
		bar.Add(1)
	}

	// Calculate metrics
	totalRows := 0
	totalSizeMB := 0.0
	for _, s := range stations {
		totalRows += s.NumReadings
		totalSizeMB += s.SizeMB
	}

	throughputRows := float64(totalRows) / elapsed.Seconds()
	throughputMB := totalSizeMB / elapsed.Seconds()

	return StrategyResult{
		Name:             strategyName,
		TotalTime:        elapsed,
		ThroughputMBps:   throughputMB,
		ThroughputRowsPS: throughputRows,
	}
}

// calculateStats computes median time from multiple iterations
func calculateStats(strategyName string, results []StrategyResult) StrategyResult {
	if len(results) == 0 {
		return StrategyResult{Name: strategyName}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	medianIdx := len(results) / 2
	median := results[medianIdx]

	return StrategyResult{
		Name:             strategyName,
		TotalTime:        median.TotalTime,
		ThroughputMBps:   median.ThroughputMBps,
		ThroughputRowsPS: median.ThroughputRowsPS,
	}
}

// printIterationStats prints detailed statistics for multiple iterations
func printIterationStats(results []StrategyResult) {
	if len(results) <= 1 {
		return
	}

	times := make([]time.Duration, len(results))
	for i, r := range results {
		times[i] = r.TotalTime
	}

	// Sort for percentile calculations
	slices.Sort(times)

	min := times[0]
	max := times[len(times)-1]
	median := times[len(times)/2]

	// Calculate mean
	var sum time.Duration
	for _, t := range times {
		sum += t
	}
	mean := sum / time.Duration(len(times))

	// Calculate standard deviation
	var variance float64
	for _, t := range times {
		diff := float64(t - mean)
		variance += diff * diff
	}
	stddev := time.Duration(math.Sqrt(variance / float64(len(times))))

	fmt.Printf("    Min: %v | Median: %v | Mean: %v | Max: %v | StdDev: %v\n",
		min.Round(time.Millisecond),
		median.Round(time.Millisecond),
		mean.Round(time.Millisecond),
		max.Round(time.Millisecond),
		stddev.Round(time.Millisecond))
}

func printResults(results []StrategyResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	for i := range results {
		results[i].Rank = i + 1
	}

	printComparisonTable(results)
}

func printComparisonTable(results []StrategyResult) {
	fmt.Println()
	bold.Println("📊 RESULTS - All Strategies Compared")
	fmt.Println()

	fastestTime := results[0].TotalTime

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Strategy", "Time", "M rows/sec", "MB/sec", "vs Fastest")

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

		vsFastest := float64(r.TotalTime) / float64(fastestTime)
		vsFastestStr := fmt.Sprintf("%.2fx", vsFastest)
		if r.Rank == 1 {
			vsFastestStr = "baseline"
		}

		table.Append(
			rankIcon,
			r.Name,
			r.TotalTime.Round(time.Millisecond).String(),
			fmt.Sprintf("%.1f", r.ThroughputRowsPS/1_000_000),
			fmt.Sprintf("%.1f", r.ThroughputMBps),
			vsFastestStr,
		)
	}

	table.Render()
}

func printConfiguration(numWorkers int, totalRows int, totalSizeMB float64, numTasks int, chunkSize int, balanced bool) {
	bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Strategies:       7 different scheduling algorithms\n")
	fmt.Printf("  Dataset:          %s temperature readings\n", formatNumber(totalRows))
	fmt.Printf("  Chunk Size:       %s rows per task\n", formatNumber(chunkSize))
	if balanced {
		fmt.Printf("  Mode:             BALANCED (uniform task sizes)\n")
	} else {
		fmt.Printf("  Mode:             IMBALANCED (realistic workload)\n")
	}
	fmt.Println()

	bold.Println("📊 Workload Details:")
	fmt.Printf("  • %s total tasks submitted to scheduler\n", formatNumber(numTasks))
	fmt.Printf("  • %s total measurements to process\n", formatNumber(totalRows))
	fmt.Printf("  • %.1f GB of data\n", totalSizeMB/1024)
	if !balanced {
		fmt.Printf("  • 3 HUGE stations (68%% of data) → %s tasks\n", formatNumber(numTasks*68/100))
		fmt.Printf("  • 27 smaller stations (32%% of data) → %s tasks\n", formatNumber(numTasks*32/100))
	} else {
		fmt.Printf("  • All tasks have uniform size (~%s rows each)\n", formatNumber(chunkSize))
	}
	fmt.Println()
}

func getStrategiesToRun(isolated string, iterations int, warmup int) []string {
	if isolated != "" {
		found := slices.Contains(allStrategies, isolated)
		if !found {
			red.Printf("Error: Unknown strategy '%s'\n", isolated)
			fmt.Println("Available strategies:", allStrategies)
			os.Exit(1)
		}
		fmt.Printf("🔬 ISOLATED MODE: Testing only '%s'\n", isolated)
		if iterations > 1 {
			fmt.Printf("  Running %d iterations with %d warmup runs\n", iterations, warmup)
		}
		fmt.Println()
		return []string{isolated}
	}
	return allStrategies
}

func makeProgressBar(strategies []string) *progressbar.ProgressBar {
	return progressbar.NewOptions(len(strategies),
		progressbar.OptionSetDescription("Testing strategies"),
		progressbar.OptionSetWidth(50),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: "░",
			BarStart:      "│",
			BarEnd:        "│",
		}),
		progressbar.OptionEnableColorCodes(true),
	)
}

func main() {
	totalRowsFlag := flag.Int("rows", 65_000_000, "Total number of rows to process (e.g., 100000000 for 100M)")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect, max 8)")
	chunkSizeFlag := flag.Int("chunk", 500, "Rows per task chunk (smaller = more tasks, default 500)")
	ciModeFlag := flag.Bool("ci", false, "CI mode: disable progress bar and animations")
	plainModeFlag := flag.Bool("plain", false, "Plain mode: disable colors and emojis")
	balancedFlag := flag.Bool("balanced", true, "Balanced mode: generate uniform-sized chunks to reduce workload imbalance")
	isolatedFlag := flag.String("isolated", "", "Isolated mode: test only one strategy (e.g., 'Work-Stealing', 'MPMC Queue')")
	iterationsFlag := flag.Int("iterations", 1, "Number of iterations to run per strategy (for statistical analysis)")
	warmupFlag := flag.Int("warmup", 0, "Number of warmup iterations before measurement")
	flag.Parse()

	ciMode := isCIMode(*ciModeFlag)
	_ = *plainModeFlag

	numWorkers := *workersFlag
	if numWorkers == 0 {
		numWorkers = min(runtime.NumCPU(), 8)
	}

	var tasks []StationData
	if *balancedFlag {
		tasks = generateBalancedStationData(*totalRowsFlag, *chunkSizeFlag)
	} else {
		tasks = generateStationData(*totalRowsFlag, *chunkSizeFlag)
	}

	totalRows := 0
	totalSizeMB := 0.0
	for _, task := range tasks {
		totalRows += task.NumReadings
		totalSizeMB += task.SizeMB
	}

	printConfiguration(numWorkers, totalRows, totalSizeMB, len(tasks), *chunkSizeFlag, *balancedFlag)

	strategies := getStrategiesToRun(*isolatedFlag, *iterationsFlag, *warmupFlag)

	results := make([]StrategyResult, 0, len(strategies))

	bold.Println("Running Benchmarks...")
	fmt.Println()

	var bar *progressbar.ProgressBar
	if !ciMode {
		bar = makeProgressBar(strategies)
	}

	for i, strategy := range strategies {
		if ciMode {
			fmt.Printf("[%d/%d] Testing strategy: %s\n", i+1, len(strategies), strategy)
		}

		if *warmupFlag > 0 {
			for w := 0; w < *warmupFlag; w++ {
				if ciMode {
					fmt.Printf("  Warmup %d/%d...\n", w+1, *warmupFlag)
				}
				_ = runStrategy(strategy, tasks, numWorkers, nil)
				runtime.GC() // Force GC between warmup runs
				time.Sleep(100 * time.Millisecond)
			}
		}

		iterationResults := make([]StrategyResult, 0, *iterationsFlag)
		for iter := 0; iter < *iterationsFlag; iter++ {
			if ciMode && *iterationsFlag > 1 {
				fmt.Printf("  Iteration %d/%d...\n", iter+1, *iterationsFlag)
			}
			if bar != nil {
				bar.Describe(fmt.Sprintf("Testing: %s", strategy))
			}

			result := runStrategy(strategy, tasks, numWorkers, bar)
			iterationResults = append(iterationResults, result)

			if iter < *iterationsFlag-1 {
				runtime.GC()
				time.Sleep(100 * time.Millisecond)
			}
		}

		var finalResult StrategyResult
		if *iterationsFlag == 1 {
			finalResult = iterationResults[0]
		} else {
			finalResult = calculateStats(strategy, iterationResults)
			if ciMode {
				printIterationStats(iterationResults)
			}
		}

		results = append(results, finalResult)

		if !ciMode {
			time.Sleep(time.Millisecond * 300)
		}

		if ciMode {
			fmt.Printf("✓ %s completed in %v (%.1f M rows/s)\n",
				strategy,
				finalResult.TotalTime.Round(time.Millisecond),
				finalResult.ThroughputRowsPS/1_000_000)
		}
	}

	fmt.Println()
	fmt.Println()

	printResults(results)
}
