package pool

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
)

// sliceProcessor orchestrates parallel processing of a slice of tasks with result ordering.
// It supports idempotent single-execution via sync.Once and buffering/channels to ensure
// safe concurrent worker processing and results collection.
//
// Type parameters:
//   - T: The task input type
//   - R: The result output type
type sliceProcessor[T, R any] struct {
	tasks     []T                 // tasks to process, in order
	processFn ProcessFunc[T, R]   // processing function for each task
	taskChan  chan task[T, int]   // channel used to dispatch tasks to workers
	resChan   chan Result[R, int] // channel for collecting processed results
	pool      *WorkerPool[T, R]   // reference to shared worker pool/config
	once      sync.Once           // ensures single execution
}

// newSliceProcessor constructs a sliceProcessor for the given tasks, process function, and pool.
func newSliceProcessor[T, R any](tasks []T, processFn ProcessFunc[T, R], wp *WorkerPool[T, R]) *sliceProcessor[T, R] {
	return &sliceProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		taskChan:  make(chan task[T, int], wp.taskBuffer),
		resChan:   make(chan Result[R, int], len(tasks)),
		pool:      wp,
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

// process executes the internal flow: spinning up workers, producing tasks, and collecting results.
func (s *sliceProcessor[T, R]) process(ctx context.Context, workerCount int) ([]R, error) {
	if len(s.tasks) == 0 {
		return []R{}, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	for range workerCount {
		g.Go(func() error {
			return worker(s.pool, ctx, s.taskChan, s.resChan, s.processFn)
		})
	}

	// Producer: feeds all tasks to taskChan
	g.Go(func() error {
		return s.produce(ctx)
	})

	results := make([]R, len(s.tasks))
	var wg sync.WaitGroup
	var err error

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = s.collect(results)
	}()

	if gErr := g.Wait(); gErr != nil {
		close(s.resChan)
		wg.Wait()
		return results, gErr
	}

	close(s.resChan)
	wg.Wait()
	return results, err
}

// produce feeds the tasks into taskChan for worker consumption; closes channel when done.
// Respects ctx cancellation and returns context error if aborted before completion.
func (s *sliceProcessor[T, R]) produce(ctx context.Context) error {
	defer close(s.taskChan)
	for idx, t := range s.tasks {
		it := &indexedTask[T]{index: idx, task: t}
		select {
		case s.taskChan <- it:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// collect reads all results from resChan, storing them in the proper order.
// If any error is encountered, it is returned after all results are collected.
func (s *sliceProcessor[T, R]) collect(results []R) error {
	var collectionErr error
	for result := range s.resChan {
		if result.Error != nil {
			collectionErr = result.Error
			continue
		}
		if result.Key >= 0 && result.Key < len(results) {
			results[result.Key] = result.Value
		}
	}
	return collectionErr
}

// mapProcessor orchestrates parallel processing of map tasks for batch processing with proper key/result mapping.
//
// Type parameters:
//   - T: The task input type
//   - R: The result output type
type mapProcessor[T, R any] struct {
	taskChan  chan task[T, string]   // channel for dispatching keyed tasks to workers
	resChan   chan Result[R, string] // channel for collecting processed results
	pool      *WorkerPool[T, R]      // reference to shared worker pool/config
	tasks     map[string]T           // input tasks map (keyed)
	processFn ProcessFunc[T, R]      // processing function for each task
	once      sync.Once              // ensures single execution
}

// newMapProcessor constructs a mapProcessor for the given keyed tasks, function, and pool.
func newMapProcessor[T, R any](tasks map[string]T, processFn ProcessFunc[T, R], wp *WorkerPool[T, R]) *mapProcessor[T, R] {
	return &mapProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		taskChan:  make(chan task[T, string], wp.taskBuffer),
		resChan:   make(chan Result[R, string], len(tasks)),
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

// process coordinates worker startup, task production, and keyed result aggregation.
func (m *mapProcessor[T, R]) process(ctx context.Context, workerCount int) (map[string]R, error) {
	if len(m.tasks) == 0 {
		return map[string]R{}, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	for range workerCount {
		g.Go(func() error {
			return worker(m.pool, ctx, m.taskChan, m.resChan, m.processFn)
		})
	}

	g.Go(func() error {
		return m.produce(ctx)
	})

	results := make(map[string]R, len(m.tasks))
	var wg sync.WaitGroup
	var err error

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = m.collect(results)
	}()

	if gerr := g.Wait(); gerr != nil {
		close(m.resChan)
		wg.Wait()
		return results, gerr
	}

	close(m.resChan)
	wg.Wait()
	return results, err
}

// produce feeds all tasks in the map to taskChan for processing, keyed by the original map key.
// Returns context error if aborted, otherwise nil.
func (m *mapProcessor[T, R]) produce(ctx context.Context) error {
	defer close(m.taskChan)
	for key, t := range m.tasks {
		kt := &keyedTask[T, string]{key: key, task: t}
		select {
		case m.taskChan <- kt:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// collect reads results for map tasks and aggregates them by the original key in the results map.
// If any result has a non-nil error, the last error is returned after collecting all results.
func (m *mapProcessor[T, R]) collect(results map[string]R) error {
	var collectionErr error
	for result := range m.resChan {
		if result.Error != nil {
			collectionErr = result.Error
			continue
		}
		results[result.Key] = result.Value
	}
	return collectionErr
}

type streamProcessor[T, R any] struct {
	taskChan         <-chan T // channel for dispatching tasks to workers
	pool             *WorkerPool[T, R]
	once             sync.Once
	internalTaskChan chan task[T, int]
	internalResChan  chan Result[R, int]
}

func newStreamProcessor[T, R any](wp *WorkerPool[T, R], taskChan <-chan T) *streamProcessor[T, R] {
	return &streamProcessor[T, R]{
		pool:             wp,
		taskChan:         taskChan,
		internalTaskChan: make(chan task[T, int], wp.taskBuffer),
		internalResChan:  make(chan Result[R, int], wp.taskBuffer),
	}
}

func (s *streamProcessor[T, R]) Process(ctx context.Context, processFn ProcessFunc[T, R], workerCount int) (resultChan <-chan R, errChan <-chan error) {
	s.once.Do(func() {
		resultChan, errChan = s.process(ctx, processFn, workerCount)
	})
	return
}

func (s *streamProcessor[T, R]) process(ctx context.Context, processFn ProcessFunc[T, R], workerCount int) (<-chan R, <-chan error) {
	resChan := make(chan R, s.pool.taskBuffer)
	errChan := make(chan error, 1)

	g, ctx := errgroup.WithContext(ctx)
	for range workerCount {
		g.Go(func() error {
			return worker(s.pool, ctx, s.internalTaskChan, s.internalResChan, processFn)
		})
	}

	g.Go(func() error {
		return s.produce(ctx)
	})

	go func() {
		defer close(resChan)
		defer close(errChan)

		var wg sync.WaitGroup
		var err error

		wg.Add(1)

		go func() {
			defer wg.Done()
			s.collect(resChan, &err)
		}()

		gerr := g.Wait()
		close(s.internalResChan)

		wg.Wait()

		if gerr != nil {
			errChan <- gerr
			return
		}

		errChan <- err
	}()

	return resChan, errChan
}

func (s *streamProcessor[T, R]) produce(ctx context.Context) error {
	defer close(s.internalTaskChan)
	for t := range s.taskChan {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			s.internalTaskChan <- &indexedTask[T]{task: t, index: -1}
		}
	}
	return nil
}

func (s *streamProcessor[T, R]) collect(resChan chan<- R, err *error) {
	for result := range s.internalResChan {
		if result.Error != nil {
			*err = result.Error
		}
		resChan <- result.Value
	}
}
