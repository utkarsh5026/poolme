package benchmarks

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

// =============================================================================
// Strategy Comparison Benchmarks - Head-to-Head Performance Tests
// =============================================================================

// BenchmarkStrategy_CPUBound_AllStrategies compares all scheduling strategies
// with CPU-bound workloads
func BenchmarkStrategy_CPUBound_AllStrategies(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(1000) // Moderate CPU work

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			// Report throughput metrics
			tasksPerOp := float64(taskCount)
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			tasksPerSec := (tasksPerOp / nsPerOp) * 1e9

			b.ReportMetric(tasksPerSec, "tasks/sec")
			b.ReportMetric(tasksPerSec/float64(workers), "tasks/sec/worker")
		})
	}
}

// BenchmarkStrategy_IOBound_AllStrategies compares all scheduling strategies
// with IO-bound workloads
func BenchmarkStrategy_IOBound_AllStrategies(b *testing.B) {
	workers := 16 // More workers for IO-bound tasks
	taskCount := 1000
	processFunc := ioBoundWork(2 * time.Millisecond)

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStrategy_Mixed_AllStrategies compares all scheduling strategies
// with mixed workloads (variable processing time)
func BenchmarkStrategy_Mixed_AllStrategies(b *testing.B) {
	workers := 8
	taskCount := 5000
	processFunc := mixedWork()

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Worker Scaling Comparison
// =============================================================================

func BenchmarkStrategy_WorkerScaling(b *testing.B) {
	workerCounts := []int{2, 4, 8, 16, 32}
	taskCount := 10000
	processFunc := cpuBoundWork(500)

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			strategies := []struct {
				name string
				opts []pool.WorkerPoolOption
			}{
				{
					name: "Channel",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithSchedulingStrategy(pool.SchedulingChannel),
					},
				},
				{
					name: "WorkStealing",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithWorkStealing(),
					},
				},
				{
					name: "MPMC",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
					},
				},
			}

			for _, strategy := range strategies {
				b.Run(strategy.name, func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						tasks := make([]int, taskCount)
						for j := range tasks {
							tasks[j] = j
						}

						wp := pool.NewWorkerPool[int, int](strategy.opts...)
						_, err := wp.Process(context.Background(), tasks, processFunc)
						if err != nil {
							b.Fatal(err)
						}
					}

					// Report efficiency metrics
					nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
					tasksPerSec := (float64(taskCount) / nsPerOp) * 1e9
					b.ReportMetric(tasksPerSec/float64(workers), "tasks/sec/worker")
				})
			}
		})
	}
}

// =============================================================================
// Load Scaling Comparison
// =============================================================================

func BenchmarkStrategy_LoadScaling(b *testing.B) {
	taskCounts := []int{100, 1000, 10000, 50000}
	workers := 8
	processFunc := cpuBoundWork(100)

	for _, taskCount := range taskCounts {
		b.Run(fmt.Sprintf("Tasks_%d", taskCount), func(b *testing.B) {
			strategies := []struct {
				name string
				opts []pool.WorkerPoolOption
			}{
				{
					name: "Channel",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithSchedulingStrategy(pool.SchedulingChannel),
					},
				},
				{
					name: "WorkStealing",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithWorkStealing(),
					},
				},
				{
					name: "MPMC_Bounded",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
					},
				},
				{
					name: "MPMC_Unbounded",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithMPMCQueue(pool.WithUnboundedQueue()),
					},
				},
			}

			for _, strategy := range strategies {
				b.Run(strategy.name, func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						tasks := make([]int, taskCount)
						for j := range tasks {
							tasks[j] = j
						}

						wp := pool.NewWorkerPool[int, int](strategy.opts...)
						_, err := wp.Process(context.Background(), tasks, processFunc)
						if err != nil {
							b.Fatal(err)
						}
					}

					// Report throughput
					nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
					tasksPerSec := (float64(taskCount) / nsPerOp) * 1e9
					b.ReportMetric(tasksPerSec, "tasks/sec")
				})
			}
		})
	}
}

// =============================================================================
// Memory Allocation Comparison
// =============================================================================

func BenchmarkStrategy_MemoryAllocations(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(100)

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Latency Distribution Comparison
// =============================================================================

func BenchmarkStrategy_LatencyDistribution(b *testing.B) {
	workers := 8
	taskCount := 5000
	processFunc := cpuBoundWork(1000)

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
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

			tasks := make([]int, taskCount)
			for j := range tasks {
				tasks[j] = j
			}

			b.ResetTimer()
			wp := pool.NewWorkerPool[int, int](strategy.opts...)
			_, err := wp.Process(context.Background(), tasks, processWithLatency)
			if err != nil {
				b.Fatal(err)
			}
			b.StopTimer()

			// Calculate and report percentiles
			if len(latencies) > 0 {
				p50 := percentile(latencies, 0.50)
				p95 := percentile(latencies, 0.95)
				p99 := percentile(latencies, 0.99)
				pMax := percentile(latencies, 1.0)

				b.ReportMetric(float64(p50.Nanoseconds()), "p50_ns")
				b.ReportMetric(float64(p95.Nanoseconds()), "p95_ns")
				b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns")
				b.ReportMetric(float64(pMax.Nanoseconds()), "max_ns")
			}
		})
	}
}

// =============================================================================
// Contention and Concurrent Submission Tests
// =============================================================================

func BenchmarkStrategy_ConcurrentSubmission(b *testing.B) {
	workers := 8
	taskCount := 10000
	submitters := []int{1, 2, 4, 8, 16} // Different numbers of concurrent submitters
	processFunc := cpuBoundWork(100)

	for _, numSubmitters := range submitters {
		b.Run(fmt.Sprintf("Submitters_%d", numSubmitters), func(b *testing.B) {
			strategies := []struct {
				name string
				opts []pool.WorkerPoolOption
			}{
				{
					name: "Channel",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithSchedulingStrategy(pool.SchedulingChannel),
					},
				},
				{
					name: "WorkStealing",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithWorkStealing(),
					},
				},
				{
					name: "MPMC",
					opts: []pool.WorkerPoolOption{
						pool.WithWorkerCount(workers),
						pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
					},
				},
			}

			for _, strategy := range strategies {
				b.Run(strategy.name, func(b *testing.B) {
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						tasks := make([]int, taskCount)
						for j := range tasks {
							tasks[j] = j
						}

						wp := pool.NewWorkerPool[int, int](strategy.opts...)

						// Split tasks among submitters
						tasksPerSubmitter := taskCount / numSubmitters
						var wg sync.WaitGroup
						wg.Add(numSubmitters)

						start := time.Now()
						for s := 0; s < numSubmitters; s++ {
							startIdx := s * tasksPerSubmitter
							endIdx := startIdx + tasksPerSubmitter
							if s == numSubmitters-1 {
								endIdx = taskCount
							}

							go func(start, end int) {
								defer wg.Done()
								subTasks := tasks[start:end]
								wp.Process(context.Background(), subTasks, processFunc)
							}(startIdx, endIdx)
						}

						wg.Wait()
						elapsed := time.Since(start)

						b.ReportMetric(float64(elapsed.Nanoseconds())/float64(taskCount), "ns/task")
					}
				})
			}
		})
	}
}

// =============================================================================
// Variable Task Complexity Tests
// =============================================================================

func BenchmarkStrategy_VariableComplexity(b *testing.B) {
	workers := 8
	taskCount := 5000

	// Variable complexity workload - some tasks are quick, some are slow
	variableComplexityWork := func(ctx context.Context, task int) (int, error) {
		// Every 10th task is 10x more expensive
		iterations := 100
		if task%10 == 0 {
			iterations = 1000
		}

		result := 0
		for i := 0; i < iterations; i++ {
			result += i * task
		}
		return result, nil
	}

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, variableComplexityWork)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// High Contention Scenarios
// =============================================================================

func BenchmarkStrategy_HighContention(b *testing.B) {
	workers := runtime.NumCPU() * 2 // Oversubscribe to create contention
	taskCount := 50000              // Many small tasks
	processFunc := cpuBoundWork(10) // Very light work to maximize scheduling overhead

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}

			// Report scheduling efficiency
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			tasksPerSec := (float64(taskCount) / nsPerOp) * 1e9
			b.ReportMetric(tasksPerSec, "tasks/sec")
		})
	}
}

// =============================================================================
// Burst Load Testing
// =============================================================================

func BenchmarkStrategy_BurstLoad(b *testing.B) {
	workers := 8
	processFunc := cpuBoundWork(500)

	// Simulate bursty workload: small batch, then large batch
	burstPattern := []int{100, 10000, 100, 10000}

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(20000)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			var totalTasks atomic.Int64

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				wp := pool.NewWorkerPool[int, int](strategy.opts...)

				for _, batchSize := range burstPattern {
					tasks := make([]int, batchSize)
					for j := range tasks {
						tasks[j] = j
					}

					_, err := wp.Process(context.Background(), tasks, processFunc)
					if err != nil {
						b.Fatal(err)
					}
					totalTasks.Add(int64(batchSize))
				}
			}
			b.StopTimer()

			// Report average throughput across bursts
			nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			avgTasksPerBurst := float64(totalTasks.Load()) / float64(b.N)
			tasksPerSec := (avgTasksPerBurst / nsPerOp) * 1e9
			b.ReportMetric(tasksPerSec, "avg_tasks/sec")
		})
	}
}

// =============================================================================
// Priority Queue Strategy Comparison (Separate from others)
// =============================================================================

func BenchmarkStrategy_PriorityQueue(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(500)

	// Priority function: higher values = higher priority
	priorityFunc := func(task int) int {
		return taskCount - task // Reverse order for priority
	}

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel_NoPriority",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "PriorityQueue",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithPriorityQueue(priorityFunc),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, processFunc)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Cache Locality Tests (Important for Work Stealing)
// =============================================================================

func BenchmarkStrategy_CacheLocality(b *testing.B) {
	workers := runtime.NumCPU()
	taskCount := 10000

	// Workload that benefits from cache locality
	// Each task accesses a slice that fits in L1 cache
	cacheLocalWork := func(ctx context.Context, task int) (int, error) {
		// Simulate working with data that fits in cache (32KB L1)
		data := make([]int, 1024) // 4KB
		sum := 0
		for i := range data {
			data[i] = task + i
			sum += data[i]
		}
		return sum, nil
	}

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks := make([]int, taskCount)
				for j := range tasks {
					tasks[j] = j
				}

				wp := pool.NewWorkerPool[int, int](strategy.opts...)
				_, err := wp.Process(context.Background(), tasks, cacheLocalWork)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Tail Latency Under Load
// =============================================================================

func BenchmarkStrategy_TailLatency(b *testing.B) {
	workers := 8
	taskCount := 10000
	processFunc := cpuBoundWork(1000)

	strategies := []struct {
		name string
		opts []pool.WorkerPoolOption
	}{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workers),
				pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount * 2)),
			},
		},
	}

	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
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

			tasks := make([]int, taskCount)
			for j := range tasks {
				tasks[j] = j
			}

			b.ResetTimer()
			wp := pool.NewWorkerPool[int, int](strategy.opts...)
			_, err := wp.Process(context.Background(), tasks, processWithLatency)
			if err != nil {
				b.Fatal(err)
			}
			b.StopTimer()

			if len(latencies) > 0 {
				p99 := percentile(latencies, 0.99)
				p999 := percentile(latencies, 0.999)
				pMax := percentile(latencies, 1.0)

				b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns")
				b.ReportMetric(float64(p999.Nanoseconds()), "p999_ns")
				b.ReportMetric(float64(pMax.Nanoseconds()), "max_ns")
			}
		})
	}
}

// Helper function to calculate percentile (from benchmark_test.go)
// Duplicated here for completeness
func percentileHelper(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

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
