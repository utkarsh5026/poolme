package pool

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
)

type sliceProcessor[T, R any] struct {
	tasks     []T
	processFn ProcessFunc[T, R]
	taskChan  chan task[T, int]
	resChan   chan Result[R, int]
	pool      *WorkerPool[T, R]
	once      sync.Once
}

func newSliceProcessor[T, R any](tasks []T, processFn ProcessFunc[T, R], wp *WorkerPool[T, R]) *sliceProcessor[T, R] {
	return &sliceProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		taskChan:  make(chan task[T, int], wp.taskBuffer),
		resChan:   make(chan Result[R, int], len(tasks)),
		pool:      wp,
	}
}

func (s *sliceProcessor[T, R]) Process(ctx context.Context, workerCount int) ([]R, error) {
	var results []R
	var processErr error

	s.once.Do(func() {
		results, processErr = s.process(ctx, workerCount)
	})

	return results, processErr
}

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

type mapProcessor[T, R any] struct {
	taskChan  chan task[T, string]
	resChan   chan Result[R, string]
	pool      *WorkerPool[T, R]
	tasks     map[string]T
	processFn ProcessFunc[T, R]
	once      sync.Once
}

func newMapProcessor[T, R any](tasks map[string]T, processFn ProcessFunc[T, R], wp *WorkerPool[T, R]) *mapProcessor[T, R] {
	return &mapProcessor[T, R]{
		tasks:     tasks,
		processFn: processFn,
		taskChan:  make(chan task[T, string], wp.taskBuffer),
		resChan:   make(chan Result[R, string], len(tasks)),
		pool:      wp,
	}
}

func (m *mapProcessor[T, R]) Process(ctx context.Context, workerCount int) (map[string]R, error) {
	var r map[string]R = make(map[string]R)
	var err error

	m.once.Do(func() {
		r, err = m.process(ctx, workerCount)
	})

	return r, err
}

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
