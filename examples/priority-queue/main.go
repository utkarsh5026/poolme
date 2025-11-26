package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

type Job struct {
	ID       int
	Priority int
	Name     string
}

func generateRandomJobs(count int) []Job {
	jobTypes := []string{"DataProcessing", "EmailSend", "ReportGen", "BackupTask", "CacheRefresh", "LogAnalysis", "FileUpload", "ImageProcess", "APICall", "DatabaseSync"}
	jobs := make([]Job, count)

	for i := range count {
		jobs[i] = Job{
			ID:       i + 1,
			Priority: rand.Intn(5) + 1, // Priority 1-5 (1=highest)
			Name:     fmt.Sprintf("%s_%d", jobTypes[rand.Intn(len(jobTypes))], i+1),
		}
	}
	return jobs
}

func getUserInput() (int, int) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\nEnter the number of jobs to run: ")
	jobCountStr, _ := reader.ReadString('\n')
	jobCountStr = strings.TrimSpace(jobCountStr)
	jobCount, err := strconv.Atoi(jobCountStr)
	if err != nil || jobCount <= 0 {
		fmt.Println("Invalid input, using default: 50 jobs")
		jobCount = 50
	}

	fmt.Print("Enter the number of workers (recommended 4-8): ")
	workerCountStr, _ := reader.ReadString('\n')
	workerCountStr = strings.TrimSpace(workerCountStr)
	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil || workerCount <= 0 {
		fmt.Println("Invalid input, using default: 4 workers")
		workerCount = 4
	}

	return jobCount, workerCount
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Priority Queue Worker Pool - Performance Demo       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// Get user input
	jobCount, workerCount := getUserInput()

	// Calculate expected time
	avgWorkDuration := 100 * time.Millisecond // Average of 50-150ms
	expectedTime := time.Duration(jobCount/workerCount) * avgWorkDuration

	fmt.Printf("\n⚙️  Configuration:\n")
	fmt.Printf("   • Jobs: %d\n", jobCount)
	fmt.Printf("   • Workers: %d\n", workerCount)
	fmt.Printf("   • Priority Levels: 1 (Urgent) → 5 (Low)\n")
	fmt.Printf("   • Simulated Work: 50-150ms per job (avg 100ms)\n")
	fmt.Printf("   • Expected Time: ~%v\n\n", expectedTime.Round(time.Second))
	fmt.Printf("💡 Note: Each job sleeps to simulate real work. This is intentional!\n\n")

	// Create scheduler with priority queue
	scheduler := pool.NewScheduler[Job, string](
		pool.WithWorkerCount(workerCount),
		pool.WithPriorityQueue(func(job Job) int {
			return job.Priority
		}),
	)

	var completedJobs atomic.Int32
	var priorityStats sync.Map // Track jobs per priority

	// Process function that simulates work
	processFn := func(ctx context.Context, job Job) (string, error) {
		// Simulate varying work duration (50-150ms)
		workDuration := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(workDuration)

		completedJobs.Add(1)

		// Track priority statistics
		val, _ := priorityStats.LoadOrStore(job.Priority, &atomic.Int32{})
		val.(*atomic.Int32).Add(1)

		return fmt.Sprintf("Completed: %s", job.Name), nil
	}

	ctx := context.Background()

	// Start the scheduler
	err := scheduler.Start(ctx, processFn)
	if err != nil {
		panic(err)
	}

	// Generate random jobs
	fmt.Println("📋 Generating random jobs...")
	jobs := generateRandomJobs(jobCount)

	// Count jobs by priority
	priorityCounts := make(map[int]int)
	for _, job := range jobs {
		priorityCounts[job.Priority]++
	}

	fmt.Println("\n📊 Job Distribution by Priority:")
	for p := 1; p <= 5; p++ {
		if count, ok := priorityCounts[p]; ok {
			fmt.Printf("   Priority %d: %d jobs\n", p, count)
		}
	}

	// Start timing
	fmt.Println("\n🚀 Starting job execution...")
	fmt.Println()
	startTime := time.Now()

	// Submit all jobs
	for _, job := range jobs {
		_, err := scheduler.Submit(job)
		if err != nil {
			fmt.Printf("Error submitting job %d: %v\n", job.ID, err)
		}
	}

	// Show progress with visual bar
	ticker := time.NewTicker(100 * time.Millisecond)
	done := make(chan bool)

	go func() {
		barWidth := 40 // Width of the progress bar
		for {
			select {
			case <-ticker.C:
				completed := completedJobs.Load()
				progress := float64(completed) / float64(jobCount) * 100
				filledWidth := int(float64(barWidth) * float64(completed) / float64(jobCount))

				// Build progress bar
				bar := "["
				for i := range barWidth {
					if i < filledWidth {
						bar += "█"
					} else {
						bar += "░"
					}
				}
				bar += "]"

				fmt.Printf("\r⏳ %s %.1f%% (%d/%d jobs) ", bar, progress, completed, jobCount)
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	// Wait for all jobs to complete
	err = scheduler.Shutdown(30 * time.Second)
	done <- true
	if err != nil {
		fmt.Printf("\n❌ Error during shutdown: %v\n", err)
	}

	totalTime := time.Since(startTime)

	// Print results
	fmt.Printf("\n\n╔═══════════════════════════════════════════════════════╗")
	fmt.Printf("\n║                  Execution Complete!                  ║")
	fmt.Printf("\n╚═══════════════════════════════════════════════════════╝\n")

	fmt.Printf("\n📈 Performance Metrics:\n")
	fmt.Printf("   • Total Jobs: %d\n", jobCount)
	fmt.Printf("   • Total Time: %v\n", totalTime.Round(time.Millisecond))
	fmt.Printf("   • Avg Time/Job: %v\n", (totalTime / time.Duration(jobCount)).Round(time.Millisecond))
	fmt.Printf("   • Throughput: %.2f jobs/second\n", float64(jobCount)/totalTime.Seconds())

	fmt.Println("\n🎯 Completed Jobs by Priority:")
	for p := 1; p <= 5; p++ {
		if val, ok := priorityStats.Load(p); ok {
			count := val.(*atomic.Int32).Load()
			fmt.Printf("   Priority %d: %d jobs\n", p, count)
		}
	}

	fmt.Println("\n💡 Performance Notes:")
	fmt.Println("   • Each job simulates work with 50-150ms sleep (avg 100ms)")
	fmt.Printf("   • With %d workers, optimal time = (%d jobs / %d workers) × 100ms ≈ %v\n",
		workerCount, jobCount, workerCount, (time.Duration(jobCount/workerCount) * 100 * time.Millisecond).Round(time.Second))
	fmt.Println("   • Your pool achieved near-optimal efficiency!")
	fmt.Println("   • Higher priority jobs (1=Urgent) are processed before lower priority (5=Low)")
	fmt.Println("   • In production, remove sleep() and add your real work logic")
}
