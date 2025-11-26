package pool

import (
	"context"
	"sync"
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
func (s *sliceProcessor[T, R]) Process(ctx context.Context, workerCount int) ([]R, error) {
	var results []R
	var processErr error

	s.once.Do(func() {
		results, processErr = s.process(ctx, workerCount)
	})

	return results, processErr
}

// process executes the internal flow using scheduling strategies with result handlers.
func (s *sliceProcessor[T, R]) process(ctx context.Context, workerCount int) ([]R, error) {
	if len(s.tasks) == 0 {
		return []R{}, nil
	}

	strategy, err := createSchedulingStrategy(s.conf, nil)
	if err != nil {
		return nil, err
	}
	defer strategy.Shutdown()

	resChan := make(chan *Result[R, int64], len(s.tasks))
	orderMap := make(map[int64]int, len(s.tasks))
	results := make([]R, len(s.tasks))

	handler := func(task *submittedTask[T, R], result *Result[R, int64]) {
		resChan <- result
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	submittedTasks := make([]*submittedTask[T, R], len(s.tasks))
	for i, task := range s.tasks {
		submittedTasks[i] = &submittedTask[T, R]{
			task:   task,
			id:     int64(i),
			future: nil,
		}
		orderMap[int64(i)] = i
	}

	submittedCount, err := strategy.SubmitBatch(submittedTasks)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer wg.Done()
			_ = strategy.Worker(ctx, int64(workerID), s.processFn, handler)
		}(i)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	var firstErr error
	for i := 0; i < submittedCount; i++ {
		result, ok := <-resChan
		if !ok {
			break
		}
		if result != nil && result.Error != nil && firstErr == nil {
			firstErr = result.Error
		}
		if result != nil {
			if idx, ok := orderMap[result.Key]; ok && idx >= 0 && idx < len(results) {
				results[idx] = result.Value
			}
		}
	}

	return results, firstErr
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
func (m *mapProcessor[T, R]) Process(ctx context.Context, workerCount int) (map[string]R, error) {
	var r map[string]R = make(map[string]R)
	var err error

	m.once.Do(func() {
		r, err = m.process(ctx, workerCount)
	})

	return r, err
}

// process coordinates worker startup, task production, and keyed result aggregation using strategies.
func (m *mapProcessor[T, R]) process(ctx context.Context, workerCount int) (map[string]R, error) {
	if len(m.tasks) == 0 {
		return map[string]R{}, nil
	}

	strategy, err := createSchedulingStrategy(m.pool.conf, nil)
	if err != nil {
		return nil, err
	}
	defer strategy.Shutdown()

	resChan := make(chan *Result[R, int64], len(m.tasks))
	keyMap := make(map[int64]string, len(m.tasks))
	results := make(map[string]R, len(m.tasks))

	handler := func(task *submittedTask[T, R], result *Result[R, int64]) {
		resChan <- result
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	submittedTasks := make([]*submittedTask[T, R], 0, len(m.tasks))
	taskID := int64(0)
	for key, task := range m.tasks {
		st := &submittedTask[T, R]{
			task:   task,
			id:     taskID,
			future: nil,
		}
		keyMap[taskID] = key
		submittedTasks = append(submittedTasks, st)
		taskID++
	}

	submittedCount, err := strategy.SubmitBatch(submittedTasks)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := range workerCount {
		go func(workerID int) {
			defer wg.Done()
			_ = strategy.Worker(ctx, int64(workerID), m.processFn, handler)
		}(i)
	}

	go func() {
		wg.Wait()
		close(resChan)
	}()

	err = collect(submittedCount, resChan, func(r *Result[R, int64]) {
		if stringKey, ok := keyMap[r.Key]; ok {
			results[stringKey] = r.Value
		}
	})
	return results, err
}

func collect[R any](n int, resChan <-chan *Result[R, int64], onResult func(r *Result[R, int64])) error {
	var firstErr error
	for range n {
		result, ok := <-resChan
		if !ok {
			break
		}
		if result != nil && result.Error != nil && firstErr == nil {
			firstErr = result.Error
		}
		if result != nil {
			onResult(result)
		}
	}

	return firstErr
}
