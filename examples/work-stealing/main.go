package main

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

// Task represents a CPU-intensive task with variable complexity
type Task struct {
	ID         int
	Complexity int    // 1-5: higher = more CPU work
	Type       string // Type of work being done
}

// WorkerStats tracks which worker processed which tasks (simulated)
type WorkerStats struct {
	tasksPerWorker map[int]int // Worker ID -> task count
}

// generateTasks creates tasks with varying complexity
// This simulates real-world scenarios where some tasks are much more expensive than others
func generateTasks(count int) []Task {
	taskTypes := []string{"Matrix", "Sort", "Search", "Parse", "Encrypt", "Compress", "Hash", "Validate"}
	tasks := make([]Task, count)

	for i := range count {
		var complexity int
		r := rand.Float64()
		if r < 0.2 {
			complexity = 4 + rand.Intn(2) // 4-5
		} else if r < 0.5 {
			complexity = 3
		} else {
			complexity = 1 + rand.Intn(2) // 1-2
		}

		tasks[i] = Task{
			ID:         i + 1,
			Complexity: complexity,
			Type:       taskTypes[rand.Intn(len(taskTypes))],
		}
	}
	return tasks
}

// simulateCPUWork performs actual CPU-intensive computation
// This is more realistic than just sleeping
func simulateCPUWork(complexity int) float64 {
	iterations := complexity * 50000
	result := 0.0

	for i := range iterations {
		result += math.Sin(float64(i)) * math.Cos(float64(i))
		result = math.Sqrt(math.Abs(result))
	}

	return result
}

// runBenchmark executes tasks with the specified scheduling strategy
func runBenchmark(ctx context.Context, tasks []Task, workerCount int, useWorkStealing bool) (time.Duration, map[int]int) {
	var completedJobs atomic.Int32
	stats := &WorkerStats{
		tasksPerWorker: make(map[int]int),
	}

	var workerPool *pool.WorkerPool[Task, float64]
	if useWorkStealing {
		workerPool = pool.NewWorkerPool[Task, float64](
			pool.WithWorkerCount(workerCount),
			pool.WithWorkStealing(),
		)
	} else {
		workerPool = pool.NewWorkerPool[Task, float64](
			pool.WithWorkerCount(workerCount),
		)
	}

	processFn := func(ctx context.Context, task Task) (float64, error) {
		result := simulateCPUWork(task.Complexity)
		completedJobs.Add(1)
		return result, nil
	}

	err := workerPool.Start(ctx, processFn)
	if err != nil {
		panic(err)
	}

	startTime := time.Now()
	for _, task := range tasks {
		_, err := workerPool.Submit(task)
		if err != nil {
			fmt.Printf("Error submitting task %d: %v\n", task.ID, err)
		}
	}

	err = workerPool.Shutdown(60 * time.Second)
	if err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
	}

	totalTime := time.Since(startTime)
	return totalTime, stats.tasksPerWorker
}

// analyzeTaskDistribution shows how tasks were distributed
func analyzeTaskDistribution(tasks []Task) {
	complexityCounts := make(map[int]int)
	for _, task := range tasks {
		complexityCounts[task.Complexity]++
	}

	fmt.Println("\nTask Distribution by Complexity:")
	for c := 1; c <= 5; c++ {
		if count, ok := complexityCounts[c]; ok {
			percentage := float64(count) / float64(len(tasks)) * 100
			fmt.Printf("   Level %d: %3d tasks (%.1f%%)\n", c, count, percentage)
		}
	}
}

// compareStrategies runs both strategies and compares results
func compareStrategies(taskCount, workerCount int) {
	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Work-Stealing vs Channel-Based Comparison            ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	tasks := generateTasks(taskCount)
	analyzeTaskDistribution(tasks)

	ctx := context.Background()

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔹 Testing: Channel-Based Strategy (Default)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("   Algorithm: All workers pull from single shared channel")
	fmt.Println("   Best for: General-purpose workloads with uniform task sizes")
	fmt.Print("\n   Running benchmark... ")

	channelTime, _ := runBenchmark(ctx, tasks, workerCount, false)
	fmt.Println("✓ Complete")

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔹 Testing: Work-Stealing Strategy")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("   Algorithm: Per-worker queues with automatic load balancing")
	fmt.Println("   Best for: CPU-intensive tasks with variable complexity")
	fmt.Print("\n   Running benchmark... ")

	stealingTime, _ := runBenchmark(ctx, tasks, workerCount, true)
	fmt.Println("✓ Complete")

	improvement := (1 - float64(stealingTime)/float64(channelTime)) * 100

	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      Performance Results                       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	fmt.Printf("\n📈 Execution Times:\n")
	fmt.Printf("   Channel-Based:  %v\n", channelTime.Round(time.Millisecond))
	fmt.Printf("   Work-Stealing:  %v\n", stealingTime.Round(time.Millisecond))

	if improvement > 0 {
		fmt.Printf("\n🚀 Performance Gain: %.2f%% faster with work-stealing!\n", improvement)
	} else {
		fmt.Printf("\n⚖️  Performance: Similar (%.2f%% difference)\n", math.Abs(improvement))
	}

	fmt.Printf("\n📊 Throughput:\n")
	fmt.Printf("   Channel-Based:  %.2f tasks/second\n", float64(taskCount)/channelTime.Seconds())
	fmt.Printf("   Work-Stealing:  %.2f tasks/second\n", float64(taskCount)/stealingTime.Seconds())
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║        Work-Stealing Scheduler - Performance Analysis         ║")
	fmt.Println("║                      PoolMe Library                           ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\n\nEnter number of tasks (default 1000): ")
	taskCountStr, _ := reader.ReadString('\n')
	taskCountStr = strings.TrimSpace(taskCountStr)
	taskCount, err := strconv.Atoi(taskCountStr)
	if err != nil || taskCount <= 0 {
		taskCount = 1000
	}

	fmt.Printf("Enter number of workers (default %d - number of CPUs): ", runtime.NumCPU())
	workerCountStr, _ := reader.ReadString('\n')
	workerCountStr = strings.TrimSpace(workerCountStr)
	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil || workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}

	fmt.Printf("\n⚙️  Configuration:\n")
	fmt.Printf("   • Tasks: %d (with variable CPU complexity)\n", taskCount)
	fmt.Printf("   • Workers: %d\n", workerCount)
	fmt.Printf("   • CPU Cores: %d\n", runtime.NumCPU())
	fmt.Println("   • Workload: Mixed complexity (20% heavy, 30% medium, 50% light)")

	// Run comparison
	compareStrategies(taskCount, workerCount)
}
