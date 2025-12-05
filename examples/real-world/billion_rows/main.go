package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/utkarsh5026/poolme/pool"
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

func printConfiguration(numWorkers int, totalRows int, totalSizeMB float64, numTasks int, chunkSize int) {
	bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Strategies:       7 different scheduling algorithms\n")
	fmt.Printf("  Dataset:          %s temperature readings\n", formatNumber(totalRows))
	fmt.Printf("  Chunk Size:       %s rows per task\n", formatNumber(chunkSize))
	fmt.Println()

	bold.Println("📊 Workload Details:")
	fmt.Printf("  • %s total tasks submitted to scheduler\n", formatNumber(numTasks))
	fmt.Printf("  • %s total measurements to process\n", formatNumber(totalRows))
	fmt.Printf("  • %.1f GB of data\n", totalSizeMB/1024)
	fmt.Printf("  • 3 HUGE stations (68%% of data) → %s tasks\n", formatNumber(numTasks*68/100))
	fmt.Printf("  • 27 smaller stations (32%% of data) → %s tasks\n", formatNumber(numTasks*32/100))
	fmt.Println()
}

func main() {
	totalRowsFlag := flag.Int("rows", 65_000_000, "Total number of rows to process (e.g., 100000000 for 100M)")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect, max 8)")
	chunkSizeFlag := flag.Int("chunk", 500, "Rows per task chunk (smaller = more tasks, default 500)")
	ciModeFlag := flag.Bool("ci", false, "CI mode: disable progress bar and animations")
	plainModeFlag := flag.Bool("plain", false, "Plain mode: disable colors and emojis")
	flag.Parse()

	ciMode := isCIMode(*ciModeFlag)
	_ = *plainModeFlag // Reserved for future plain mode implementation

	numWorkers := *workersFlag
	if numWorkers == 0 {
		numWorkers = min(runtime.NumCPU(), 8)
	}

	tasks := generateStationData(*totalRowsFlag, *chunkSizeFlag)

	totalRows := 0
	totalSizeMB := 0.0
	for _, task := range tasks {
		totalRows += task.NumReadings
		totalSizeMB += task.SizeMB
	}

	printConfiguration(numWorkers, totalRows, totalSizeMB, len(tasks), *chunkSizeFlag)

	strategies := []string{
		"Channel",
		"Work-Stealing",
		"MPMC Queue",
		"LMAX Disruptor",
		"Priority Queue",
		"Skip List",
		"Bitmask",
	}

	results := make([]StrategyResult, 0, len(strategies))

	bold.Println("Running Benchmarks...")
	fmt.Println()

	var bar *progressbar.ProgressBar
	if !ciMode {
		bar = progressbar.NewOptions(len(strategies),
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

	for i, strategy := range strategies {
		if ciMode {
			fmt.Printf("[%d/%d] Testing strategy: %s\n", i+1, len(strategies), strategy)
		}
		if bar != nil {
			bar.Describe(fmt.Sprintf("Testing: %s", strategy))
		}
		result := runStrategy(strategy, tasks, numWorkers, bar)
		results = append(results, result)

		if !ciMode {
			time.Sleep(time.Millisecond * 300)
		}

		if ciMode {
			fmt.Printf("✓ %s completed in %v (%.1f M rows/s)\n",
				strategy,
				result.TotalTime.Round(time.Millisecond),
				result.ThroughputRowsPS/1_000_000)
		}
	}

	fmt.Println()
	fmt.Println()

	printResults(results)
}
