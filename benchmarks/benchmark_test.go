package benchmarks

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

// =============================================================================
// Benchmark Workload Generators
// =============================================================================

// cpuBoundWork simulates a CPU-intensive operation
func cpuBoundWork(iterations int) func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		result := 0
		for i := 0; i < iterations; i++ {
			result += i * task
		}
		return result, nil
	}
}

// ioBoundWork simulates an I/O operation with a delay
func ioBoundWork(delay time.Duration) func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		select {
		case <-time.After(delay):
			return task * 2, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// mixedWork simulates a realistic workload with variable processing time
func mixedWork() func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		// Simulate variable processing time (0-10ms)
		delay := time.Duration(task%10) * time.Millisecond
		time.Sleep(delay)

		// Do some computation
		result := 0
		for i := 0; i < 1000; i++ {
			result += i
		}
		return result + task, nil
	}
}

// errorProneWork occasionally returns errors for retry testing
func errorProneWork(errorRate float64) func(ctx context.Context, task int) (int, error) {
	var attempts sync.Map
	return func(ctx context.Context, task int) (int, error) {
		val, _ := attempts.LoadOrStore(task, new(atomic.Int32))
		count := val.(*atomic.Int32).Add(1)

		if count == 1 && rand.Float64() < errorRate {
			return 0, fmt.Errorf("simulated error for task %d", task)
		}
		return task * 2, nil
	}
}

// =============================================================================
// Throughput Benchmarks - Core Performance Metrics
// =============================================================================

func BenchmarkComprehensive_ThroughputWorkerScaling(b *testing.B) {
	workerCounts := []int{2, 4, 8, 16, 32, 64}
	taskCount := 10000

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			processFunc := cpuBoundWork(100)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			// Report custom metrics
			tasksPerOp := float64(taskCount)
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			tasksPerSec := (tasksPerOp / nsPerOp) * 1e9

			b.ReportMetric(tasksPerSec, "tasks/sec")
			b.ReportMetric(tasksPerSec/float64(workers), "tasks/sec/worker")
		})
	}
}

func BenchmarkComprehensive_ThroughputLoadScaling(b *testing.B) {
	taskCounts := []int{100, 1000, 10000, 100000}
	workers := 8

	for _, taskCount := range taskCounts {
		b.Run(fmt.Sprintf("tasks_%d", taskCount), func(b *testing.B) {
			processFunc := cpuBoundWork(100)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			tasksPerOp := float64(taskCount)
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			tasksPerSec := (tasksPerOp / nsPerOp) * 1e9

			b.ReportMetric(tasksPerSec, "tasks/sec")
		})
	}
}

func BenchmarkComprehensive_ThroughputBufferSize(b *testing.B) {
	bufferMultipliers := []int{0, 1, 2, 4, 8}
	workers := 8
	taskCount := 10000

	for _, multiplier := range bufferMultipliers {
		bufferSize := workers * multiplier
		if multiplier == 0 {
			bufferSize = 0
		}

		b.Run(fmt.Sprintf("buffer_%dx", multiplier), func(b *testing.B) {
			processFunc := cpuBoundWork(100)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				opts := []pool.WorkerPoolOption{pool.WithWorkerCount(workers)}
				if bufferSize > 0 {
					opts = append(opts, pool.WithTaskBuffer(bufferSize))
				}

				wp := pool.NewWorkerPool[int, int](opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Processing Mode Comparison Benchmarks
// =============================================================================

func BenchmarkComprehensive_ModesProcess(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_ModesProcessMap(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make(map[string]int, taskCount)
		for j := 0; j < taskCount; j++ {
			tasks[fmt.Sprintf("task_%d", j)] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.ProcessMap(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_ModesProcessStream(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		taskChan := make(chan int, 100)

		// Producer goroutine
		go func() {
			for j := 0; j < taskCount; j++ {
				taskChan <- j
			}
			close(taskChan)
		}()

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		resultChan, errChan := wp.ProcessStream(context.Background(), taskChan, processFunc)

		// Consume results
		count := 0
		for {
			select {
			case _, ok := <-resultChan:
				if !ok {
					resultChan = nil
				} else {
					count++
				}
			case err := <-errChan:
				if err != nil {
					b.Fatal(err)
				}
			}

			if resultChan == nil {
				break
			}
		}

		if count != taskCount {
			b.Fatalf("expected %d results, got %d", taskCount, count)
		}
	}
}

// =============================================================================
// Feature Overhead Benchmarks
// =============================================================================

func BenchmarkComprehensive_FeaturesBaseline(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_FeaturesWithRetry(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := errorProneWork(0.1) // 10% error rate

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](
			pool.WithWorkerCount(workers),
			pool.WithRetryPolicy(3, 10*time.Millisecond),
		)
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_FeaturesWithRateLimit(b *testing.B) {
	workers := 8
	taskCount := 1000 // Smaller task count for rate limiting
	processFunc := cpuBoundWork(100)

	rateLimits := []int{100, 500, 1000, 5000}

	for _, rateLimit := range rateLimits {
		b.Run(fmt.Sprintf("rate_%d_per_sec", rateLimit), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](
					pool.WithWorkerCount(workers),
					pool.WithRateLimit(float64(rateLimit), rateLimit/10),
				)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkComprehensive_FeaturesWithHooks(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	var hookCalls atomic.Int64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](
			pool.WithWorkerCount(workers),
			pool.WithBeforeTaskStart(func(task int) {
				hookCalls.Add(1)
			}),
			pool.WithOnTaskEnd(func(task int, result int, err error) {
				hookCalls.Add(1)
			}),
		)
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_FeaturesContinueOnError(b *testing.B) {
	workers := 8
	taskCount := 10000

	// Task that errors on every 100th item
	processFunc := func(ctx context.Context, task int) (int, error) {
		if task%100 == 0 {
			return 0, fmt.Errorf("error on task %d", task)
		}
		result := 0
		for i := 0; i < 100; i++ {
			result += i
		}
		return result, nil
	}

	b.Run("stop_on_error", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tasks := make([]int, taskCount)
			for j := range tasks {
				tasks[j] = j
			}

			wp := pool.NewWorkerPool[int, int](
				pool.WithWorkerCount(workers),
				pool.WithContinueOnError(false),
			)
			wp.Process(context.Background(), tasks, processFunc)
			// Ignore error as we expect it
		}
	})

	b.Run("continue_on_error", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tasks := make([]int, taskCount)
			for j := range tasks {
				tasks[j] = j
			}

			wp := pool.NewWorkerPool[int, int](
				pool.WithWorkerCount(workers),
				pool.WithContinueOnError(true),
			)
			wp.Process(context.Background(), tasks, processFunc)
			// Ignore error as some tasks are expected to fail
		}
	})
}

// =============================================================================
// Workload Type Benchmarks
// =============================================================================

func BenchmarkComprehensive_WorkloadCPUBound(b *testing.B) {
	iterations := []int{100, 1000, 10000}
	workers := 8
	taskCount := 1000

	for _, iter := range iterations {
		b.Run(fmt.Sprintf("iterations_%d", iter), func(b *testing.B) {
			processFunc := cpuBoundWork(iter)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkComprehensive_WorkloadIOBound(b *testing.B) {
	delays := []time.Duration{
		1 * time.Millisecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
	}
	workers := 8
	taskCount := 100 // Smaller count for I/O bound

	for _, delay := range delays {
		b.Run(fmt.Sprintf("delay_%s", delay), func(b *testing.B) {
			processFunc := ioBoundWork(delay)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkComprehensive_WorkloadMixed(b *testing.B) {
	workers := 8
	taskCount := 1000
	processFunc := mixedWork()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Memory Benchmarks
// =============================================================================

func BenchmarkComprehensive_MemoryAllocationPerTask(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()

	// Calculate allocations per task from total memory
	totalBytes := testing.AllocsPerRun(1, func() {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}
		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		wp.Process(context.Background(), tasks, processFunc)
	})
	b.ReportMetric(totalBytes/float64(taskCount), "allocs/task")
}

func BenchmarkComprehensive_MemoryTaskScaling(b *testing.B) {
	taskCounts := []int{100, 1000, 10000, 100000}
	workers := 8
	processFunc := cpuBoundWork(100)

	for _, taskCount := range taskCounts {
		b.Run(fmt.Sprintf("tasks_%d", taskCount), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Latency Benchmarks
// =============================================================================

func BenchmarkComprehensive_LatencyDistribution(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(1000)

	var latencies []time.Duration
	var mu sync.Mutex

	processWithLatency := func(ctx context.Context, task int) (int, error) {
		start := time.Now()
		result, err := processFunc(ctx, task)
		elapsed := time.Since(start)

		mu.Lock()
		latencies = append(latencies, elapsed)
		mu.Unlock()

		return result, err
	}

	b.ResetTimer()

	tasks := make([]int, taskCount)
	for j := range tasks {
		tasks[j] = j
	}

	wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
	_, err := wp.Process(context.Background(), tasks, processWithLatency)
	if err != nil {
		b.Fatal(err)
	}

	b.StopTimer()

	// Calculate percentiles
	if len(latencies) > 0 {
		p50 := percentile(latencies, 0.50)
		p95 := percentile(latencies, 0.95)
		p99 := percentile(latencies, 0.99)

		b.ReportMetric(float64(p50.Nanoseconds()), "p50_ns")
		b.ReportMetric(float64(p95.Nanoseconds()), "p95_ns")
		b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns")
	}
}

// =============================================================================
// Scenario Benchmarks (Real-World Use Cases)
// =============================================================================

func BenchmarkComprehensive_ScenarioWebAPIProcessing(b *testing.B) {
	// Simulates processing web API responses
	processFunc := func(ctx context.Context, task int) (int, error) {
		// Simulate network I/O (1-5ms)
		time.Sleep(time.Duration(1+task%5) * time.Millisecond)

		// Simulate JSON parsing and processing (CPU work)
		result := 0
		for i := 0; i < 1000; i++ {
			result += i * task
		}

		return result, nil
	}

	workers := 16 // Higher worker count for I/O-bound work
	taskCount := 500

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](
			pool.WithWorkerCount(workers),
			pool.WithRateLimit(1000, 100), // Rate limit to avoid overwhelming API
		)
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComprehensive_ScenarioDataPipeline(b *testing.B) {
	// Simulates ETL data pipeline processing
	processFunc := func(ctx context.Context, task int) (int, error) {
		// Extract (read)
		time.Sleep(100 * time.Microsecond)

		// Transform (CPU work)
		result := task
		for i := 0; i < 5000; i++ {
			result = (result*31 + i) % 1000000
		}

		// Load (write)
		time.Sleep(100 * time.Microsecond)

		return result, nil
	}

	workers := runtime.GOMAXPROCS(0)
	taskCount := 10000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](
			pool.WithWorkerCount(workers),
			pool.WithTaskBuffer(workers*2),
		)
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Report throughput
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	tasksPerSec := (float64(taskCount) / nsPerOp) * 1e9
	b.ReportMetric(tasksPerSec, "records/sec")
}

func BenchmarkComprehensive_ScenarioBatchJobProcessing(b *testing.B) {
	// Simulates batch job processing with retries
	processFunc := func(ctx context.Context, task int) (int, error) {
		// Simulate work that occasionally fails
		if task%50 == 0 {
			return 0, fmt.Errorf("transient error")
		}

		// Simulate processing
		result := 0
		for i := 0; i < 10000; i++ {
			result += i
		}

		return result + task, nil
	}

	workers := 8
	taskCount := 1000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](
			pool.WithWorkerCount(workers),
			pool.WithRetryPolicy(3, 100*time.Millisecond),
			pool.WithContinueOnError(true),
		)
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Comparison Benchmarks
// =============================================================================

func BenchmarkComprehensive_ComparisonSequential(b *testing.B) {
	taskCount := 1000
	processFunc := cpuBoundWork(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		results := make([]int, taskCount)
		for j, task := range tasks {
			result, err := processFunc(context.Background(), task)
			if err != nil {
				b.Fatal(err)
			}
			results[j] = result
		}
	}
}

func BenchmarkComprehensive_ComparisonWorkerPool(b *testing.B) {
	taskCount := 1000
	processFunc := cpuBoundWork(100)
	workers := 8

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks := make([]int, taskCount)
		for j := range tasks {
			tasks[j] = j
		}

		wp := pool.NewWorkerPool[int, int](pool.WithWorkerCount(workers))
		_, err := wp.Process(context.Background(), tasks, processFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Create a copy and sort
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Simple bubble sort (fine for benchmark data)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	index := int(math.Ceil(float64(len(sorted)) * p))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}
