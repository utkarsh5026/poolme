package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

// Task represents a work item to be processed
type Task struct {
	ID          int
	SubmitterID int
	Data        string
}

// Result represents the processed result
type Result struct {
	TaskID      int
	ProcessedAt time.Time
	Result      string
}

// generateTasks creates a batch of tasks
func generateTasks(count int, submitterID int) []Task {
	tasks := make([]Task, count)
	dataTypes := []string{"order", "payment", "user", "inventory", "analytics", "notification"}

	for i := 0; i < count; i++ {
		tasks[i] = Task{
			ID:          submitterID*count + i + 1,
			SubmitterID: submitterID,
			Data:        dataTypes[rand.Intn(len(dataTypes))],
		}
	}
	return tasks
}

// processTask simulates work with variable duration
func processTask(ctx context.Context, task Task) (Result, error) {
	// Simulate processing time (5-15ms)
	processingTime := time.Duration(5+rand.Intn(10)) * time.Millisecond
	time.Sleep(processingTime)

	return Result{
		TaskID:      task.ID,
		ProcessedAt: time.Now(),
		Result:      fmt.Sprintf("Processed %s data from submitter %d", task.Data, task.SubmitterID),
	}, nil
}

// runWithMPMC executes tasks using MPMC queue with multiple concurrent submitters
func runWithMPMC(ctx context.Context, submitterCount, tasksPerSubmitter, workerCount int, bounded bool, capacity int) (time.Duration, *PerformanceMetrics) {
	var opts []pool.WorkerPoolOption
	if bounded {
		opts = []pool.WorkerPoolOption{
			pool.WithWorkerCount(workerCount),
			pool.WithMPMCQueue(pool.WithBoundedQueue(capacity)),
		}
	} else {
		opts = []pool.WorkerPoolOption{
			pool.WithWorkerCount(workerCount),
			pool.WithMPMCQueue(pool.WithUnboundedQueue()),
		}
	}

	scheduler := pool.NewScheduler[Task, Result](opts...)

	err := scheduler.Start(ctx, processTask)
	if err != nil {
		panic(err)
	}

	metrics := &PerformanceMetrics{
		submissionTimes: make([]time.Duration, submitterCount),
	}

	var wg sync.WaitGroup
	startTime := time.Now()

	// Multiple concurrent submitters
	wg.Add(submitterCount)
	for s := 0; s < submitterCount; s++ {
		go func(submitterID int) {
			defer wg.Done()

			tasks := generateTasks(tasksPerSubmitter, submitterID)
			submissionStart := time.Now()

			for _, task := range tasks {
				_, err := scheduler.Submit(task)
				if err != nil {
					fmt.Printf("Submitter %d: Error submitting task %d: %v\n", submitterID, task.ID, err)
					metrics.failedSubmissions.Add(1)
					continue
				}
				metrics.successfulSubmissions.Add(1)
			}

			metrics.submissionTimes[submitterID] = time.Since(submissionStart)
		}(s)
	}

	wg.Wait()

	// Shutdown and wait for all tasks to complete
	err = scheduler.Shutdown(60 * time.Second)
	if err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
	}

	totalTime := time.Since(startTime)
	return totalTime, metrics
}

// runWithChannel executes tasks using channel strategy with multiple concurrent submitters
func runWithChannel(ctx context.Context, submitterCount, tasksPerSubmitter, workerCount int) (time.Duration, *PerformanceMetrics) {
	scheduler := pool.NewScheduler[Task, Result](
		pool.WithWorkerCount(workerCount),
		pool.WithTaskBuffer(1024),
	)

	err := scheduler.Start(ctx, processTask)
	if err != nil {
		panic(err)
	}

	metrics := &PerformanceMetrics{
		submissionTimes: make([]time.Duration, submitterCount),
	}

	var wg sync.WaitGroup
	startTime := time.Now()

	// Multiple concurrent submitters
	wg.Add(submitterCount)
	for s := 0; s < submitterCount; s++ {
		go func(submitterID int) {
			defer wg.Done()

			tasks := generateTasks(tasksPerSubmitter, submitterID)
			submissionStart := time.Now()

			for _, task := range tasks {
				_, err := scheduler.Submit(task)
				if err != nil {
					fmt.Printf("Submitter %d: Error submitting task %d: %v\n", submitterID, task.ID, err)
					metrics.failedSubmissions.Add(1)
					continue
				}
				metrics.successfulSubmissions.Add(1)
			}

			metrics.submissionTimes[submitterID] = time.Since(submissionStart)
		}(s)
	}

	wg.Wait()

	// Shutdown and wait for all tasks to complete
	err = scheduler.Shutdown(60 * time.Second)
	if err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
	}

	totalTime := time.Since(startTime)
	return totalTime, metrics
}

// PerformanceMetrics tracks performance statistics
type PerformanceMetrics struct {
	successfulSubmissions atomic.Int64
	failedSubmissions     atomic.Int64
	submissionTimes       []time.Duration
}

// getAverageSubmissionTime calculates average submission time across all submitters
func (m *PerformanceMetrics) getAverageSubmissionTime() time.Duration {
	if len(m.submissionTimes) == 0 {
		return 0
	}

	var total time.Duration
	for _, t := range m.submissionTimes {
		total += t
	}
	return total / time.Duration(len(m.submissionTimes))
}

// getMaxSubmissionTime returns the maximum submission time
func (m *PerformanceMetrics) getMaxSubmissionTime() time.Duration {
	if len(m.submissionTimes) == 0 {
		return 0
	}

	max := m.submissionTimes[0]
	for _, t := range m.submissionTimes[1:] {
		if t > max {
			max = t
		}
	}
	return max
}

// compareStrategies runs both strategies and compares results
func compareStrategies(submitterCount, tasksPerSubmitter, workerCount int) {
	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║         MPMC Queue vs Channel-Based Comparison                ║")
	fmt.Println("║         Multi-Producer Concurrent Submission Test             ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	totalTasks := submitterCount * tasksPerSubmitter
	ctx := context.Background()

	fmt.Printf("\n⚙️  Configuration:\n")
	fmt.Printf("   • Total Tasks: %d\n", totalTasks)
	fmt.Printf("   • Concurrent Submitters: %d\n", submitterCount)
	fmt.Printf("   • Tasks per Submitter: %d\n", tasksPerSubmitter)
	fmt.Printf("   • Workers: %d\n", workerCount)
	fmt.Printf("   • CPU Cores: %d\n", runtime.NumCPU())

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔹 Testing: Channel-Based Strategy (Default)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("   Algorithm: Single buffered channel with mutex protection")
	fmt.Println("   Submitters contend on channel send operations")
	fmt.Print("\n   Running benchmark... ")

	channelTime, channelMetrics := runWithChannel(ctx, submitterCount, tasksPerSubmitter, workerCount)
	fmt.Println("✓ Complete")

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔹 Testing: MPMC Queue Strategy (Unbounded)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("   Algorithm: Lock-free ring buffer with atomic operations")
	fmt.Println("   Multiple producers can submit without contention")
	fmt.Print("\n   Running benchmark... ")

	mpmcTime, mpmcMetrics := runWithMPMC(ctx, submitterCount, tasksPerSubmitter, workerCount, false, 0)
	fmt.Println("✓ Complete")

	improvement := (1 - float64(mpmcTime)/float64(channelTime)) * 100

	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      Performance Results                       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	fmt.Printf("\n📈 Total Execution Time:\n")
	fmt.Printf("   Channel-Based:  %v\n", channelTime.Round(time.Millisecond))
	fmt.Printf("   MPMC Queue:     %v\n", mpmcTime.Round(time.Millisecond))

	if improvement > 0 {
		fmt.Printf("\n🚀 Performance Gain: %.2f%% faster with MPMC!\n", improvement)
	} else {
		fmt.Printf("\n⚖️  Performance: Channel-based was %.2f%% faster\n", -improvement)
	}

	fmt.Printf("\n📊 Throughput:\n")
	fmt.Printf("   Channel-Based:  %.2f tasks/second\n", float64(totalTasks)/channelTime.Seconds())
	fmt.Printf("   MPMC Queue:     %.2f tasks/second\n", float64(totalTasks)/mpmcTime.Seconds())

	fmt.Printf("\n⏱️  Average Submission Time (per submitter):\n")
	fmt.Printf("   Channel-Based:  %v\n", channelMetrics.getAverageSubmissionTime().Round(time.Microsecond))
	fmt.Printf("   MPMC Queue:     %v\n", mpmcMetrics.getAverageSubmissionTime().Round(time.Microsecond))

	fmt.Printf("\n📉 Max Submission Time:\n")
	fmt.Printf("   Channel-Based:  %v\n", channelMetrics.getMaxSubmissionTime().Round(time.Microsecond))
	fmt.Printf("   MPMC Queue:     %v\n", mpmcMetrics.getMaxSubmissionTime().Round(time.Microsecond))

	fmt.Printf("\n✅ Task Completion:\n")
	fmt.Printf("   Channel-Based:  %d successful, %d failed\n",
		channelMetrics.successfulSubmissions.Load(),
		channelMetrics.failedSubmissions.Load())
	fmt.Printf("   MPMC Queue:     %d successful, %d failed\n",
		mpmcMetrics.successfulSubmissions.Load(),
		mpmcMetrics.failedSubmissions.Load())
}

// demonstrateBoundedQueue shows bounded queue behavior
func demonstrateBoundedQueue(submitterCount, tasksPerSubmitter, workerCount, capacity int) {
	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║             Bounded MPMC Queue Demonstration                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")

	totalTasks := submitterCount * tasksPerSubmitter
	ctx := context.Background()

	fmt.Printf("\n⚙️  Configuration:\n")
	fmt.Printf("   • Total Tasks: %d\n", totalTasks)
	fmt.Printf("   • Queue Capacity: %d (bounded)\n", capacity)
	fmt.Printf("   • Concurrent Submitters: %d\n", submitterCount)
	fmt.Printf("   • Workers: %d\n", workerCount)

	fmt.Print("\n   Running bounded queue test... ")
	boundedTime, boundedMetrics := runWithMPMC(ctx, submitterCount, tasksPerSubmitter, workerCount, true, capacity)
	fmt.Println("✓ Complete")

	fmt.Printf("\n📈 Results:\n")
	fmt.Printf("   Total Time: %v\n", boundedTime.Round(time.Millisecond))
	fmt.Printf("   Throughput: %.2f tasks/second\n", float64(totalTasks)/boundedTime.Seconds())
	fmt.Printf("   Successful: %d tasks\n", boundedMetrics.successfulSubmissions.Load())
	fmt.Printf("   Failed:     %d tasks\n", boundedMetrics.failedSubmissions.Load())

	if boundedMetrics.failedSubmissions.Load() > 0 {
		fmt.Println("\n⚠️  Note: Some submissions failed due to bounded queue capacity")
		fmt.Println("   Consider increasing queue capacity or worker count for better throughput")
	} else {
		fmt.Println("\n✅ All tasks submitted successfully within bounded queue limits")
	}
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          MPMC Queue - Performance Analysis                     ║")
	fmt.Println("║                    PoolMe Library                             ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("This demo showcases the MPMC (Multi-Producer Multi-Consumer) queue")
	fmt.Println("strategy's ability to handle multiple concurrent task submitters")
	fmt.Println("with minimal contention.")

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\n\nEnter number of concurrent submitters (default 10): ")
	submitterStr, _ := reader.ReadString('\n')
	submitterStr = strings.TrimSpace(submitterStr)
	submitterCount, err := strconv.Atoi(submitterStr)
	if err != nil || submitterCount <= 0 {
		submitterCount = 10
	}

	fmt.Print("Enter tasks per submitter (default 100): ")
	tasksStr, _ := reader.ReadString('\n')
	tasksStr = strings.TrimSpace(tasksStr)
	tasksPerSubmitter, err := strconv.Atoi(tasksStr)
	if err != nil || tasksPerSubmitter <= 0 {
		tasksPerSubmitter = 100
	}

	fmt.Printf("Enter number of workers (default %d - number of CPUs): ", runtime.NumCPU())
	workerStr, _ := reader.ReadString('\n')
	workerStr = strings.TrimSpace(workerStr)
	workerCount, err := strconv.Atoi(workerStr)
	if err != nil || workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}

	// Run comparison
	compareStrategies(submitterCount, tasksPerSubmitter, workerCount)

	// Ask if user wants to see bounded queue demo
	fmt.Print("\n\nWould you like to see bounded queue demonstration? (y/n): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "y" || response == "yes" {
		capacity := submitterCount * tasksPerSubmitter / 4 // Set capacity to 25% of total tasks
		if capacity < 64 {
			capacity = 64
		}
		demonstrateBoundedQueue(submitterCount, tasksPerSubmitter, workerCount, capacity)
	}

	fmt.Println("\n" + strings.Repeat("═", 65))
	fmt.Println("Demo Complete!")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("• MPMC queue excels with many concurrent submitters")
	fmt.Println("• Lock-free design reduces contention")
	fmt.Println("• Bounded mode provides backpressure control")
	fmt.Println("• Unbounded mode offers flexibility for bursty workloads")
	fmt.Println(strings.Repeat("═", 65))
}
