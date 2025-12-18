package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/pprof"
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
	ChunkID     int    // Chunk number for this station
	NumReadings int    // Number of readings in this chunk
	Complexity  string // Tier: Huge, Large, Medium, Small, Tiny
	StartIndex  int    // Starting index for seed offset
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

func processStation(_ context.Context, station StationData) (StationStats, error) {
	start := time.Now()

	minTemp := 100.0
	maxTemp := -100.0
	sumTemp := 0.0

	state := uint64(hashString(station.StationName)) + uint64(station.StartIndex)
	if state == 0 {
		state = 1 // Xorshift must not be zero
	}

	for i := 0; i < station.NumReadings; i++ {
		// 2. Inline Xorshift Algorithm (High performance randomness)
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		// 3. Convert uint64 to float range [-20, 40]
		// This effectively replaces rng.Float64()*60 - 20
		// We use the remainder to get a range [0, 600) then divide by 10 for decimal
		// This avoids float multiplication overhead

		// Optimize: (x % 600) gives 0-599. Subtract 200 gives -200 to 399. Divide by 10 gives -20.0 to 39.9
		rawTemp := int(x % 600)             // 0 to 599
		temp := float64(rawTemp-200) / 10.0 // -20.0 to 39.9

		// (No math.Round needed because we constructed it from integers!)

		if temp < minTemp {
			minTemp = temp
		}
		if temp > maxTemp {
			maxTemp = temp
		}
		sumTemp += temp

		// Simulated heavy load (Keep this if you need to simulate CPU work)
		if i%1000 == 0 {
			_ = math.Sin(float64(i)) * math.Cos(temp)
		}
	}

	avgTemp := sumTemp / float64(station.NumReadings)

	return StationStats{
		StationName: station.StationName,
		NumReadings: station.NumReadings,
		MinTemp:     minTemp,
		MaxTemp:     maxTemp,
		AvgTemp:     math.Round(avgTemp*10) / 10, // Round only once at the end
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

		stationName := stationNames[stationIdx%len(stationNames)]
		stationIdx++

		tasks = append(tasks, StationData{
			StationName: stationName,
			ChunkID:     chunkID,
			NumReadings: chunkRows,
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
		tier    string
	}{
		// HUGE stations - 68% total
		{"Beijing", 22.77, "Huge"},
		{"Tokyo", 18.22, "Huge"},
		{"Delhi", 27.32, "Huge"},

		// Large stations - 27% total
		{"Shanghai", 7.59, "Large"},
		{"Mumbai", 6.83, "Large"},
		{"Cairo", 5.77, "Large"},
		{"Moscow", 6.38, "Large"},

		// Medium stations - 4% total
		{"London", 0.68, "Medium"},
		{"Paris", 0.58, "Medium"},
		{"Berlin", 0.64, "Medium"},
		{"Rome", 0.53, "Medium"},
		{"Madrid", 0.59, "Medium"},
		{"Amsterdam", 0.43, "Medium"},
		{"Brussels", 0.47, "Medium"},
		{"Vienna", 0.52, "Medium"},

		// Small/Tiny stations - 1% total
		{"Oslo", 0.068, "Small"},
		{"Helsinki", 0.058, "Small"},
		{"Stockholm", 0.064, "Small"},
		{"Copenhagen", 0.062, "Small"},
		{"Dublin", 0.053, "Small"},
		{"Lisbon", 0.059, "Small"},
		{"Athens", 0.067, "Small"},
		{"Warsaw", 0.061, "Small"},
		{"Prague", 0.056, "Small"},
		{"Budapest", 0.055, "Small"},
		{"Reykjavik", 0.018, "Tiny"},
		{"Luxembourg", 0.023, "Tiny"},
		{"Monaco", 0.012, "Tiny"},
		{"Vaduz", 0.014, "Tiny"},
		{"Andorra", 0.017, "Tiny"},
	}

	tasks := make([]StationData, 0)

	for _, dist := range stationDistribution {
		stationRows := int(float64(totalRows) * dist.percent / 100.0)

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
				Complexity:  dist.tier,
				StartIndex:  startIdx,
			})
		}
	}

	return tasks
}

// generatePriorityStationData creates tasks ordered to test priority reordering.
// It generates data using the imbalanced distribution, then reverses the order
// so that small tasks are submitted first and large tasks last.
func generatePriorityStationData(totalRows int, chunkSize int) []StationData {
	tasks := generateStationData(totalRows, chunkSize)
	slices.Reverse(tasks) // Submit small first, large last
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

type Runner struct {
	strategy   string
	numWorkers int
	stations   []StationData
}

func newRunner(strategyName string, stations []StationData, numWorkers int) *Runner {
	return &Runner{
		strategy:   strategyName,
		numWorkers: numWorkers,
		stations:   stations,
	}
}

func (r *Runner) Run(bar *progressbar.ProgressBar) StrategyResult {
	ctx := context.Background()
	wPool := r.selectStrategy()
	start := time.Now()

	// Process all tasks using the batch Process method
	// This handles submission and result collection internally, avoiding deadlocks
	_, err := wPool.Process(ctx, r.stations, processStation)
	if err != nil {
		_, _ = red.Printf("Error processing %s: %v\n", r.strategy, err)
		return StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)

	if bar != nil {
		_ = bar.Add(1)
	}

	totalRows := 0
	for _, s := range r.stations {
		totalRows += s.NumReadings
	}

	throughputRows := float64(totalRows) / elapsed.Seconds()

	return StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputMBps:   0,
		ThroughputRowsPS: throughputRows,
	}
}

func (r *Runner) selectStrategy() *pool.WorkerPool[StationData, StationStats] {
	switch r.strategy {
	case "Work-Stealing":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithPriorityQueue(func(a, b StationData) bool {
				return a.NumReadings > b.NumReadings
			}),
		)
	case "Skip List":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithSkipList(func(a, b StationData) bool {
				return a.NumReadings > b.NumReadings
			}),
		)
	case "Bitmask":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithLmax(),
		)
	default:
		return pool.NewWorkerPool[StationData, StationStats](
			pool.WithWorkerCount(r.numWorkers),
		)
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

	mini := times[0]
	maxi := times[len(times)-1]
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
		mini.Round(time.Millisecond),
		median.Round(time.Millisecond),
		mean.Round(time.Millisecond),
		maxi.Round(time.Millisecond),
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
	bold.Println("📊 TASK THROUGHPUT RESULTS - Strategy Comparison")
	fmt.Println()

	fastestTime := results[0].TotalTime

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Strategy", "Time", "M tasks/sec", "MB/sec", "vs Fastest")

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

		_ = table.Append(
			rankIcon,
			r.Name,
			r.TotalTime.Round(time.Millisecond).String(),
			fmt.Sprintf("%.1f", r.ThroughputRowsPS/1_000_000),
			fmt.Sprintf("%.1f", r.ThroughputMBps),
			vsFastestStr,
		)
	}

	_ = table.Render()
}

func printConfiguration(numWorkers int, totalRows int, numTasks int, chunkSize int, balanced bool) {
	_, _ = bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Strategies:       7 different scheduling algorithms\n")
	fmt.Printf("  Total Items:      %s items to process\n", formatNumber(totalRows))
	fmt.Printf("  Chunk Size:       %s items per task\n", formatNumber(chunkSize))
	if balanced {
		fmt.Printf("  Mode:             BALANCED (uniform task sizes)\n")
	} else {
		fmt.Printf("  Mode:             IMBALANCED (realistic workload)\n")
	}
	fmt.Println()

	_, _ = bold.Println("📊 Workload Details:")
	fmt.Printf("  • %s concurrent tasks submitted to scheduler\n", formatNumber(numTasks))
	fmt.Printf("  • %s total items to process\n", formatNumber(totalRows))
	if !balanced {
		fmt.Printf("  • 3 HUGE tasks (68%% of workload) → %s tasks\n", formatNumber(numTasks*68/100))
		fmt.Printf("  • 27 smaller tasks (32%% of workload) → %s tasks\n", formatNumber(numTasks*32/100))
	} else {
		fmt.Printf("  • All tasks have uniform size (~%s items each)\n", formatNumber(chunkSize))
	}
	fmt.Println()
}

func getStrategiesToRun(isolated string, iterations int, warmup int) []string {
	if isolated != "" {
		found := slices.Contains(allStrategies, isolated)
		if !found {
			_, _ = red.Printf("Error: Unknown strategy '%s'\n", isolated)
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
		progressbar.OptionSetWriter(os.Stderr),          // Use stderr for better terminal support
		progressbar.OptionThrottle(65*time.Millisecond), // Reduce update frequency
		progressbar.OptionClearOnFinish(),               // Clear progress bar when done
	)
}

func main() {
	// Enable ANSI escape sequences on Windows for progress bar support
	enableWindowsANSI()

	totalRowsFlag := flag.Int("rows", 65_000_000, "Total number of tasks to process (e.g., 100000000 for 100M tasks)")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect, max 8)")
	chunkSizeFlag := flag.Int("chunk", 500, "Items per task chunk (smaller = more concurrent tasks, default 500)")
	balancedFlag := flag.Bool("balanced", true, "Balanced mode: generate uniform-sized chunks to reduce workload imbalance")
	strategyFlag := flag.String("strategy", "", "Run a specific scheduler strategy (e.g., 'Work-Stealing', 'MPMC Queue'). If empty, runs all strategies")
	iterationsFlag := flag.Int("iterations", 1, "Number of iterations to run per strategy (for statistical analysis)")
	warmupFlag := flag.Int("warmup", 0, "Number of warmup iterations before measurement")
	priorityFlag := flag.Bool("priority", false, "Priority mode: reverse task order to test priority-based schedulers reordering tasks")
	cpuProfileFlag := flag.String("cpuprofile", "", "Write CPU profile to file")
	memProfileFlag := flag.String("memprofile", "", "Write memory profile to file")
	flag.Parse()

	if *cpuProfileFlag != "" {
		f, err := os.Create(*cpuProfileFlag)
		if err != nil {
			_, _ = red.Printf("Error creating CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				_, _ = red.Printf("Error closing memory file: %v\n", err)
			}
		}(f)

		if err := pprof.StartCPUProfile(f); err != nil {
			_, _ = red.Printf("Error starting CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
		fmt.Printf("CPU profiling enabled, writing to: %s\n", *cpuProfileFlag)
	}

	var memProfileFile *os.File
	if *memProfileFlag != "" {
		var err error
		memProfileFile, err = os.Create(*memProfileFlag)
		if err != nil {
			_, _ = red.Printf("Error creating memory profile: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			runtime.GC()
			if err := pprof.WriteHeapProfile(memProfileFile); err != nil {
				_, _ = red.Printf("Error writing memory profile: %v\n", err)
			}
			if err := memProfileFile.Close(); err != nil {
				_, _ = red.Printf("Error closing memory file: %v\n", err)
			}
		}()
		fmt.Printf("Memory profiling enabled, writing to: %s\n", *memProfileFlag)
	}

	numWorkers := min(runtime.NumCPU(), *workersFlag)
	var tasks []StationData
	if *priorityFlag {
		tasks = generatePriorityStationData(*totalRowsFlag, *chunkSizeFlag)
	} else if *balancedFlag {
		tasks = generateBalancedStationData(*totalRowsFlag, *chunkSizeFlag)
	} else {
		tasks = generateStationData(*totalRowsFlag, *chunkSizeFlag)
	}

	totalRows := 0
	for _, task := range tasks {
		totalRows += task.NumReadings
	}

	printConfiguration(numWorkers, totalRows, len(tasks), *chunkSizeFlag, *balancedFlag)

	strategies := getStrategiesToRun(*strategyFlag, *iterationsFlag, *warmupFlag)

	results := make([]StrategyResult, 0, len(strategies))

	_, _ = bold.Println("Running Benchmarks...")
	fmt.Println()

	bar := makeProgressBar(strategies)

	for _, strategy := range strategies {
		if *warmupFlag > 0 {
			for w := 0; w < *warmupFlag; w++ {
				r := newRunner(strategy, tasks, numWorkers)
				_ = r.Run(nil)
				runtime.GC() // Force GC between warmup runs
				time.Sleep(100 * time.Millisecond)
			}
		}

		iterationResults := make([]StrategyResult, 0, *iterationsFlag)
		for iter := 0; iter < *iterationsFlag; iter++ {
			bar.Describe(fmt.Sprintf("Testing: %s", strategy))

			r := newRunner(strategy, tasks, numWorkers)
			iterationResults = append(iterationResults, r.Run(bar))

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
			printIterationStats(iterationResults)
		}

		results = append(results, finalResult)
		time.Sleep(time.Millisecond * 300)
	}

	fmt.Println()
	fmt.Println()

	printResults(results)
}
