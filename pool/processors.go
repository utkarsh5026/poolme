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
