package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

// ============================================
// ATOMIC COUNTERS
// ============================================

var (
	// Global counters for pipeline stages
	totalReads          atomic.Int64
	totalTransforms     atomic.Int64
	totalWrites         atomic.Int64
	totalValidations    atomic.Int64
	totalBytesProcessed atomic.Int64
)

// DataRecord represents a raw data record for processing
type DataRecord struct {
	ID              int               `json:"id"`
	Timestamp       time.Time         `json:"timestamp"`
	Data            map[string]string `json:"data"`
	Size            int               `json:"size"` // Size in bytes
	NeedsValidation bool              `json:"needs_validation"`
}

// PipelineTask represents a data pipeline processing task
type PipelineTask struct {
	ID             int
	Record         DataRecord
	ReadLatencyMs  int    // Simulated read I/O latency
	WriteLatencyMs int    // Simulated write I/O latency
	TransformCPU   int    // CPU work for transformation
	Stage          string // "etl", "streaming", "batch"
}

// PipelineResult represents the result of processing a pipeline task
type PipelineResult struct {
	ID             int
	ProcessTime    time.Duration
	ReadTime       time.Duration
	TransformTime  time.Duration
	WriteTime      time.Duration
	ValidationTime time.Duration
	BytesProcessed int
	Stage          string
}

// StageBreakdown holds pipeline stage timing data
type StageBreakdown struct {
	AvgReadTime      time.Duration
	AvgTransformTime time.Duration
	AvgWriteTime     time.Duration
}

// Global storage for stage breakdowns (accessed by printPipelineBreakdown)
var stageBreakdowns = make(map[string]StageBreakdown)

// generateETLTasks creates tasks simulating an ETL pipeline
func generateETLTasks(count int, avgRecordSizeKB int, readLatency int, writeLatency int, cpuWork int) []PipelineTask {
	tasks := make([]PipelineTask, count)

	for i := range count {
		sizeBytes := avgRecordSizeKB * 1024
		roll := rand.Intn(100)
		if roll < 30 {
			sizeBytes /= 2
		} else if roll >= 80 {
			sizeBytes *= 3
		}

		needsValidation := rand.Intn(100) < 30
		dataMap := make(map[string]string)
		numFields := 5 + rand.Intn(10)
		for j := range numFields {
			dataMap[fmt.Sprintf("field_%d", j)] = fmt.Sprintf("value_%d_%d", i, j)
		}

		record := DataRecord{
			ID:              i,
			Timestamp:       time.Now(),
			Data:            dataMap,
			Size:            sizeBytes,
			NeedsValidation: needsValidation,
		}

		tasks[i] = PipelineTask{
			ID:             i,
			Record:         record,
			ReadLatencyMs:  readLatency,
			WriteLatencyMs: writeLatency,
			TransformCPU:   cpuWork,
			Stage:          "etl",
		}
	}
	return tasks
}

// generateStreamingTasks simulates streaming data pipeline (faster, smaller records)
func generateStreamingTasks(count int, avgRecordSizeKB int, readLatency int, writeLatency int, cpuWork int) []PipelineTask {
	tasks := make([]PipelineTask, count)

	for i := range count {
		sizeBytes := avgRecordSizeKB * 1024
		sizeBytes += rand.Intn(sizeBytes/2) - sizeBytes/4 // ±25% variance

		needsValidation := rand.Intn(100) < 10

		dataMap := make(map[string]string)
		numFields := 3 + rand.Intn(5)
		for j := range numFields {
			dataMap[fmt.Sprintf("field_%d", j)] = fmt.Sprintf("value_%d_%d", i, j)
		}

		record := DataRecord{
			ID:              i,
			Timestamp:       time.Now(),
			Data:            dataMap,
			Size:            sizeBytes,
			NeedsValidation: needsValidation,
		}

		tasks[i] = PipelineTask{
			ID:             i,
			Record:         record,
			ReadLatencyMs:  readLatency,
			WriteLatencyMs: writeLatency,
			TransformCPU:   cpuWork / 2, // Less CPU work in streaming
			Stage:          "streaming",
		}
	}
	return tasks
}

// generateBatchTasks simulates batch processing (larger records, more validation)
func generateBatchTasks(count int, avgRecordSizeKB int, readLatency int, writeLatency int, cpuWork int) []PipelineTask {
	tasks := make([]PipelineTask, count)

	for i := range count {
		sizeBytes := avgRecordSizeKB * 1024
		roll := rand.Intn(100)
		if roll < 20 {
			sizeBytes /= 2
		} else if roll >= 70 {
			sizeBytes *= 5 // Very large records
		}

		needsValidation := rand.Intn(100) < 60

		dataMap := make(map[string]string)
		numFields := 10 + rand.Intn(20)
		for j := range numFields {
			dataMap[fmt.Sprintf("field_%d", j)] = fmt.Sprintf("value_%d_%d", i, j)
		}

		record := DataRecord{
			ID:              i,
			Timestamp:       time.Now(),
			Data:            dataMap,
			Size:            sizeBytes,
			NeedsValidation: needsValidation,
		}

		tasks[i] = PipelineTask{
			ID:             i,
			Record:         record,
			ReadLatencyMs:  readLatency,
			WriteLatencyMs: writeLatency,
			TransformCPU:   cpuWork * 2, // More CPU work in batch
			Stage:          "batch",
		}
	}
	return tasks
}

// simulateRead simulates reading data from source (database, file, S3, etc.)
func simulateRead(ctx context.Context, sizeBytes int, latencyMs int) time.Duration {
	start := time.Now()
	sizeLatency := sizeBytes / 10000 // +1ms per 10KB
	totalLatency := latencyMs + sizeLatency

	jitter := float64(totalLatency) * 0.2
	actualLatency := max(totalLatency+int(float64(rand.Intn(int(jitter*2)))-jitter), 1)

	timer := time.NewTimer(time.Duration(actualLatency) * time.Millisecond)
	select {
	case <-timer.C:
		totalReads.Add(1)
	case <-ctx.Done():
		timer.Stop()
		return time.Since(start)
	}

	return time.Since(start)
}

// performTransformation simulates data transformation (parsing, validation, enrichment)
func performTransformation(record DataRecord, cpuWork int, needsValidation bool) (time.Duration, time.Duration) {
	start := time.Now()
	state := uint32(record.ID)
	if state == 0 {
		state = 1
	}

	result := float64(xorShift(cpuWork, state))
	transformTime := time.Since(start)

	var validationTime time.Duration
	if needsValidation {
		validStart := time.Now()

		_, _ = json.Marshal(record)
		for range record.Data {
			result += math.Sin(float64(state))
			state++
		}

		validationTime = time.Since(validStart)
		totalValidations.Add(1)
	}

	totalTransforms.Add(1)
	_ = result

	return transformTime, validationTime
}

// simulateWrite simulates writing data to destination (database, file, S3, etc.)
func simulateWrite(ctx context.Context, sizeBytes int, latencyMs int) time.Duration {
	start := time.Now()

	sizeLatency := sizeBytes / 8000 // +1ms per 8KB
	totalLatency := latencyMs + sizeLatency

	jitter := float64(totalLatency) * 0.25
	actualLatency := max(totalLatency+int(float64(rand.Intn(int(jitter*2)))-jitter), 1)

	timer := time.NewTimer(time.Duration(actualLatency) * time.Millisecond)
	select {
	case <-timer.C:
		totalWrites.Add(1)
		totalBytesProcessed.Add(int64(sizeBytes))
	case <-ctx.Done():
		timer.Stop()
		return time.Since(start)
	}
	return time.Since(start)
}

func processPipelineTask(ctx context.Context, task PipelineTask) (PipelineResult, error) {
	start := time.Now()
	result := PipelineResult{
		ID:             task.ID,
		Stage:          task.Stage,
		BytesProcessed: task.Record.Size,
	}

	readTime := simulateRead(ctx, task.Record.Size, task.ReadLatencyMs)
	result.ReadTime = readTime

	transformTime, validationTime := performTransformation(
		task.Record,
		task.TransformCPU,
		task.Record.NeedsValidation,
	)
	result.TransformTime = transformTime
	result.ValidationTime = validationTime

	writeTime := simulateWrite(ctx, task.Record.Size, task.WriteLatencyMs)
	result.WriteTime = writeTime

	result.ProcessTime = time.Since(start)

	return result, nil
}

// PipelineRunner implements the benchmark runner for pipeline workloads
type PipelineRunner struct {
	strategy   string
	numWorkers int
	tasks      []PipelineTask
	results    []PipelineResult
}

func (r *PipelineRunner) Run() runner.StrategyResult {
	ctx := context.Background()

	wPool := SelectStrategy(r.strategy, StrategyConfig[PipelineTask, PipelineResult]{
		NumWorkers: r.numWorkers,
		Comparator: func(a, b PipelineTask) bool {
			return a.Record.Size < b.Record.Size // Process smaller records first
		},
	})

	start := time.Now()
	results, err := wPool.Process(ctx, r.tasks, processPipelineTask)
	if err != nil {
		_, _ = runner.Red.Printf("Error processing %s: %v\n", r.strategy, err)
		return runner.StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)
	r.results = results

	latencies := make([]time.Duration, len(results))
	var totalReadTime, totalTransformTime, totalWriteTime time.Duration
	var totalBytes int64

	for i, res := range results {
		latencies[i] = res.ProcessTime
		totalReadTime += res.ReadTime
		totalTransformTime += res.TransformTime
		totalWriteTime += res.WriteTime
		totalBytes += int64(res.BytesProcessed)
	}

	slices.Sort(latencies)

	taskCount := len(results)
	avgLatency := elapsed / time.Duration(taskCount)
	p95Latency := latencies[int(float64(len(latencies))*0.95)]
	p99Latency := latencies[int(float64(len(latencies))*0.99)]
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()
	throughputMBPS := float64(totalBytes) / (1024 * 1024) / elapsed.Seconds()

	stageBreakdowns[r.strategy] = StageBreakdown{
		AvgReadTime:      totalReadTime / time.Duration(taskCount),
		AvgTransformTime: totalTransformTime / time.Duration(taskCount),
		AvgWriteTime:     totalWriteTime / time.Duration(taskCount),
	}

	return runner.StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputRowsPS: throughputTasksPS,
		ThroughputMBPS:   throughputMBPS,
		AvgLatency:       avgLatency,
		P95Latency:       p95Latency,
		P99Latency:       p99Latency,
	}
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatNumber formats a number with comma separators
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			_, _ = result.WriteString(",")
		}
		_, _ = result.WriteString(string(c))
	}
	return result.String()
}

// printPipelineConfiguration prints the pipeline benchmark configuration
func printPipelineConfiguration(numWorkers int, numTasks int, workload string, recordSize int) {
	cp := ConfigPrinter{
		Title:      "Data Pipeline Benchmark Configuration:",
		NumWorkers: numWorkers,
		NumTasks:   numTasks,
		Workload:   workload,
		CustomParams: map[string]string{
			"Record Size": fmt.Sprintf("~%d KB average", recordSize),
		},
		WorkloadDesc: map[string]string{
			WorkloadETL:       "  • ETL Pipeline: Read → Transform → Validate → Write\n  • Mixed record sizes (50% avg, 30% small, 20% large)\n  • 30% records require validation\n",
			WorkloadStreaming: "  • Streaming Pipeline: Fast, lightweight processing\n  • Smaller, more uniform records (±25% variance)\n  • 10% records require validation\n",
			WorkloadBatch:     "  • Batch Processing: Large records, heavy validation\n  • High size variance (20% small, 70% large, 10% very large)\n  • 60% records require validation\n",
		},
	}
	cp.Print()
}

// printPipelineResults prints the pipeline benchmark results
func printPipelineResults(results []runner.StrategyResult) {
	rr := ResultsRenderer{
		Title:          "DATA PIPELINE PERFORMANCE",
		ShowMBPS:       true,
		ShowAvgLatency: true,
		ShowVsFastest:  false,
	}
	rr.PrintComparisonTable(results)

	printPipelineBreakdown(results)
	printPipelineOperationsSummary()
}

// printPipelineBreakdown prints the pipeline stage breakdown table
func printPipelineBreakdown(results []runner.StrategyResult) {
	fmt.Println()
	_, _ = bold.Println("📈 Pipeline Stage Breakdown (Average Times)")
	fmt.Println()

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Scheduler", "Read (Extract)", "Transform", "Write (Load)", "Total")

	for _, r := range results {
		breakdown := stageBreakdowns[r.Name]
		_ = table.Append([]string{
			r.Name,
			breakdown.AvgReadTime.Round(time.Microsecond).String(),
			breakdown.AvgTransformTime.Round(time.Microsecond).String(),
			breakdown.AvgWriteTime.Round(time.Microsecond).String(),
			r.AvgLatency.Round(time.Microsecond).String(),
		})
	}

	_ = table.Render()
}

// printPipelineOperationsSummary prints the summary of pipeline operations
func printPipelineOperationsSummary() {
	fmt.Println()
	_, _ = bold.Println("📈 Pipeline Operations Summary:")
	fmt.Printf("  Records Read:       %s\n", formatNumber(int(totalReads.Load())))
	fmt.Printf("  Transformations:    %s\n", formatNumber(int(totalTransforms.Load())))
	fmt.Printf("  Validations:        %s\n", formatNumber(int(totalValidations.Load())))
	fmt.Printf("  Records Written:    %s\n", formatNumber(int(totalWrites.Load())))
	fmt.Printf("  Bytes Processed:    %s\n", formatBytes(totalBytesProcessed.Load()))
	fmt.Println()
}
