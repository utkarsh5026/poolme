package pool

import (
	"context"
	"errors"
	"sync"

	"github.com/utkarsh5026/poolme/internal/scheduler"
	"github.com/utkarsh5026/poolme/internal/types"
)

// sliceProcessor orchestrates parallel processing of a slice of tasks with result ordering.
// Uses scheduling strategies for task distribution with result handlers for efficient collection.
//
// Type parameters:
//   - T: The task input type
//   - R: The result output type
type sliceProcessor[T, R any] struct {
	tasks     []T                    // tasks to process, in order
	processFn ProcessFunc[T, R]      // processing function for each task
	conf      *processorConfig[T, R] // reference to shared processor configuration
	once      sync.Once              // ensures single execution
}

// newSliceProcessor constructs a sliceProcessor for the given tasks, process function, and pool.
func newSliceProcessor[T, R any](tasks []T, processFn ProcessFunc[T, R], conf *processorConfig[T, R]) *sliceProcessor[T, R] {
	return &sliceProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		conf:      conf,
	}
}

// Process processes the slice of tasks using the specified number of workers and returns
// a results slice (in the original order) or the first error encountered. Safe for repeated calls.
func (s *sliceProcessor[T, R]) Process(ctx context.Context) ([]R, error) {
	var results []R
	var processErr error

	s.once.Do(func() {
		results, processErr = s.process(ctx)
	})

	return results, processErr
}

// process executes the internal flow using scheduling strategies with result handlers.
func (s *sliceProcessor[T, R]) process(ctx context.Context) ([]R, error) {
	if len(s.tasks) == 0 {
		return []R{}, nil
	}

	orderMap := make(map[int64]int, len(s.tasks))
	results := make([]R, len(s.tasks))

	submittedTasks := make([]*submittedTask[T, R], len(s.tasks))
	for i, task := range s.tasks {
		submittedTasks[i] = types.NewSubmittedTask[T, R](task, int64(i), nil)
		orderMap[int64(i)] = i
	}

	err := runScheduler(ctx, s.conf, submittedTasks, s.processFn, func(r *Result[R, int64]) {
		if idx, ok := orderMap[r.Key]; ok && idx >= 0 && idx < len(results) {
			results[idx] = r.Value
		}
	})

	return results, err
}

// mapProcessor orchestrates parallel processing of map tasks for batch processing with proper key/result mapping.
// Uses scheduling strategies for task distribution with result handlers for efficient collection.
//
// Type parameters:
//   - T: The task input type
//   - R: The result output type
type mapProcessor[T, R any] struct {
	tasks     map[string]T      // input tasks map (keyed)
	processFn ProcessFunc[T, R] // processing function for each task
	pool      *WorkerPool[T, R] // reference to shared worker pool/config
	once      sync.Once         // ensures single execution
}

// newMapProcessor constructs a mapProcessor for the given keyed tasks, function, and pool.
func newMapProcessor[T, R any](tasks map[string]T, processFn ProcessFunc[T, R], wp *WorkerPool[T, R]) *mapProcessor[T, R] {
	return &mapProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		pool:      wp,
	}
}

// Process launches workers to process the map's tasks in parallel and collects results in a keyed map.
// Safe for repeated calls; only the first call will execute due to sync.Once.
func (m *mapProcessor[T, R]) Process(ctx context.Context) (map[string]R, error) {
	var r map[string]R = make(map[string]R)
	var err error

	m.once.Do(func() {
		r, err = m.process(ctx)
	})

	return r, err
}

// process coordinates worker startup, task production, and keyed result aggregation using strategies.
func (m *mapProcessor[T, R]) process(ctx context.Context) (map[string]R, error) {
	if len(m.tasks) == 0 {
		return map[string]R{}, nil
	}

	keyMap := make(map[int64]string, len(m.tasks))
	results := make(map[string]R, len(m.tasks))

	submittedTasks := make([]*submittedTask[T, R], 0, len(m.tasks))
	taskID := int64(0)
	for key, task := range m.tasks {
		st := types.NewSubmittedTask[T, R](task, taskID, nil)
		keyMap[taskID] = key
		submittedTasks = append(submittedTasks, st)
		taskID++
	}

	err := runScheduler(ctx, m.pool.conf, submittedTasks, m.processFn, func(r *Result[R, int64]) {
		if stringKey, ok := keyMap[r.Key]; ok {
			results[stringKey] = r.Value
		}
	})
	return results, err
}

// collect receives results from a channel and invokes a callback for each result.
// It collects exactly n results and returns the first error encountered, if any.
//
// The function continues collecting all n results even after encountering an error,
// ensuring proper cleanup and allowing the callback to process all results.
// Context errors (Canceled, DeadlineExceeded) are prioritized over other errors.
func collect[R any](n int, resChan <-chan *Result[R, int64], onResult func(r *Result[R, int64])) error {
	debugLog("collect: expecting %d results", n)
	var firstErr error
	var contextErr error
	collected := 0
	for range n {
		result, ok := <-resChan
		if !ok {
			break
		}
		collected++
		if result != nil && result.Error != nil {
			// Prioritize context errors over other errors
			if errors.Is(result.Error, context.Canceled) || errors.Is(result.Error, context.DeadlineExceeded) {
				if contextErr == nil {
					contextErr = result.Error
				}
			} else if firstErr == nil {
				firstErr = result.Error
			}
		}
		if result != nil {
			onResult(result)
		}
	}

	// Return context error if present, otherwise return first error
	if contextErr != nil {
		return contextErr
	}
	return firstErr
}

// startWorkers spawns the specified number of worker goroutines and manages their lifecycle.
//
// Each worker pulls tasks from the scheduling strategy and processes them using the provided
// process function and result handler. The function automatically closes the result channel
// once all workers have completed.
func startWorkers[T, R any](ctx context.Context, workers int, strategy schedulingStrategy[T, R], f ProcessFunc[T, R], h resultHandler[T, R], resChan chan *Result[R, int64]) {
	debugLog("startWorkers: spawning %d workers", workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(workerID int) {
			debugLog("worker %d: started", workerID)
			defer func() {
				debugLog("worker %d: exiting", workerID)
				wg.Done()
			}()
			_ = strategy.Worker(ctx, int64(workerID), f, h)
		}(i)
	}

	go func() {
		wg.Wait()
		debugLog("startWorkers: all workers done, closing result channel")
		close(resChan)
	}()
}

// runScheduler orchestrates the complete task processing workflow using a scheduling strategy.
//
// This function creates the scheduling strategy, submits all tasks, starts workers, and collects
// results. It automatically handles strategy cleanup and error propagation.
func runScheduler[T, R any](ctx context.Context, conf *processorConfig[T, R], tasks []*submittedTask[T, R], p ProcessFunc[T, R], onResult func(r *Result[R, int64])) error {
	debugLog("runScheduler: creating scheduling strategy for %d tasks", len(tasks))
	s, err := scheduler.CreateSchedulingStrategy(conf, nil)
	if err != nil {
		debugLog("runScheduler: failed to create strategy: %v", err)
		return err
	}

	var shutdownOnce sync.Once
	defer func() {
		shutdownOnce.Do(func() {
			s.Shutdown()
		})
	}()

	resChan := make(chan *Result[R, int64], len(tasks))
	handler := func(task *submittedTask[T, R], result *Result[R, int64]) {
		resChan <- result
	}

	startWorkers(ctx, conf.WorkerCount, s, p, handler, resChan)

	go func() {
		<-ctx.Done()
		debugLog("runScheduler: context cancelled, triggering strategy shutdown")
		shutdownOnce.Do(func() {
			s.Shutdown()
		})
	}()

	submitErrChan := make(chan error, 1)
	submittedCountChan := make(chan int, 1)

	go func() {
		debugLog("runScheduler: submitting batch of %d tasks", len(tasks))
		submittedCount, err := s.SubmitBatch(tasks)
		if err != nil {
			submitErrChan <- err
			return
		}
		submittedCountChan <- submittedCount
	}()

	var submittedCount int
	var submissionErr error

	// First, try to get submission result or context done
	select {
	case submissionErr = <-submitErrChan:
		debugLog("runScheduler: got submission error: %v", submissionErr)
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return submissionErr
		}
	case submittedCount = <-submittedCountChan:
		debugLog("runScheduler: submitted %d tasks", submittedCount)
	case <-ctx.Done():
		return ctx.Err()
	}

	err = collect(submittedCount, resChan, onResult)

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
