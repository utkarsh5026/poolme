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

	strategy, err := createSchedulingStrategy[T, R](s.conf, nil)
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

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer wg.Done()
			_ = strategy.Worker(ctx, int64(workerID), s.processFn, handler)
		}(i)
	}

	submittedCount := 0
	for i, task := range s.tasks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		st := &submittedTask[T, R]{
			task:   task,
			id:     int64(i),
			future: nil,
		}
		orderMap[int64(i)] = i
		if err := strategy.Submit(st); err != nil {
			return nil, err
		}
		submittedCount++
	}

	// Collect results with ordering
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

	strategy, err := createSchedulingStrategy[T, R](m.pool.conf, nil)
	if err != nil {
		return nil, err
	}
	defer strategy.Shutdown()

	// Create result collection channel and key mapping
	resChan := make(chan *Result[R, int64], len(m.tasks))
	keyMap := make(map[int64]string, len(m.tasks))
	results := make(map[string]R, len(m.tasks))

	handler := func(task *submittedTask[T, R], result *Result[R, int64]) {
		resChan <- result
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer wg.Done()
			_ = strategy.Worker(ctx, int64(workerID), m.processFn, handler)
		}(i)
	}

	taskID := int64(0)
	submittedCount := 0
	for key, task := range m.tasks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		st := &submittedTask[T, R]{
			task:   task,
			id:     taskID,
			future: nil,
		}
		keyMap[taskID] = key
		taskID++
		if err := strategy.Submit(st); err != nil {
			return nil, err
		}
		submittedCount++
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
			if stringKey, ok := keyMap[result.Key]; ok {
				results[stringKey] = result.Value
			}
		}
	}

	return results, firstErr
}

type streamProcessor[T, R any] struct {
	taskChan <-chan T          // external input channel for tasks
	pool     *WorkerPool[T, R] // reference to worker pool config
	once     sync.Once         // ensures single execution
}

func newStreamProcessor[T, R any](wp *WorkerPool[T, R], taskChan <-chan T) *streamProcessor[T, R] {
	return &streamProcessor[T, R]{
		pool:     wp,
		taskChan: taskChan,
	}
}

func (s *streamProcessor[T, R]) Process(ctx context.Context, processFn ProcessFunc[T, R], workerCount int) (resultChan <-chan R, errChan <-chan error) {
	s.once.Do(func() {
		resultChan, errChan = s.process(ctx, processFn, workerCount)
	})
	return
}

func (s *streamProcessor[T, R]) process(ctx context.Context, processFn ProcessFunc[T, R], workerCount int) (<-chan R, <-chan error) {
	resChan := make(chan R, s.pool.conf.taskBuffer)
	errChan := make(chan error, 1)

	// Create ephemeral strategy
	strategy, err := createSchedulingStrategy[T, R](s.pool.conf, nil)
	if err != nil {
		close(resChan)
		errChan <- err
		close(errChan)
		return resChan, errChan
	}

	// Internal result channel for strategy workers
	internalResChan := make(chan *Result[R, int64], s.pool.conf.taskBuffer)

	// Shared result handler - sends to internal channel
	handler := func(task *submittedTask[T, R], result *Result[R, int64]) {
		internalResChan <- result
	}

	ctx, cancel := context.WithCancel(ctx)

	// Start workers
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			defer wg.Done()
			_ = strategy.Worker(ctx, int64(workerID), processFn, handler)
		}(i)
	}

	// Task submission goroutine - reads from external channel and submits to strategy
	go func() {
		defer strategy.Shutdown()
		defer cancel()

		taskID := int64(0)
		for task := range s.taskChan {
			st := &submittedTask[T, R]{
				task:   task,
				id:     taskID,
				future: nil,
			}
			taskID++

			select {
			case <-ctx.Done():
				return
			default:
				if err := strategy.Submit(st); err != nil {
					return
				}
			}
		}
	}()

	// Goroutine to close internal channel after workers finish
	go func() {
		wg.Wait()
		close(internalResChan)
	}()

	// Result forwarding goroutine - reads from internal channel concurrently with workers
	go func() {
		defer close(resChan)
		defer close(errChan)

		var firstErr error
		for result := range internalResChan {
			if result.Error != nil && firstErr == nil {
				firstErr = result.Error
			}
			select {
			case resChan <- result.Value:
			case <-ctx.Done():
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				errChan <- firstErr
				return
			}
		}

		errChan <- firstErr
	}()

	return resChan, errChan
}
