package scheduler

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

// channelStrategy provides a simple scheduling strategy that distributes tasks
// to a set of worker-specific channels in a round-robin fashion.
//
// Each worker goroutine reads from its own channel to execute submitted tasks.
type channelStrategy[T any, R any] struct {
	config       *ProcessorConfig[T, R]            // Processor configuration parameters.
	taskChans    []chan *types.SubmittedTask[T, R] // Per-worker task channels.
	counter      atomic.Int64                      // Atomic counter for round-robin channel selection.
	quit         chan struct{}                     // Channel to signal batch goroutines to quit.
	wg           sync.WaitGroup                    // WaitGroup to wait for running batch submissions.
	affinityFunc func(t T) (hash string)
}

// newChannelStrategy creates a new instance of channelStrategy.
// It initializes the required number of task channels (one per worker),
// using the maximum of runtime.NumCPU() and configured WorkerCount.
func newChannelStrategy[T any, R any](conf *ProcessorConfig[T, R]) *channelStrategy[T, R] {
	n := max(runtime.NumCPU(), conf.WorkerCount)
	c := &channelStrategy[T, R]{
		config:       conf,
		taskChans:    make([]chan *types.SubmittedTask[T, R], n),
		quit:         make(chan struct{}),
		affinityFunc: conf.AffinityFunc,
	}

	for i := range n {
		c.taskChans[i] = make(chan *types.SubmittedTask[T, R], conf.TaskBuffer)
	}

	return c
}

// Submit submits a single task to the next worker channel using round-robin scheduling.
// Returns nil since this operation cannot fail synchronously.
func (s *channelStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	select {
	case s.taskChans[s.next(task)] <- task:
		return nil
	case <-s.quit:
		return context.Canceled
	}
}

// SubmitBatch submits a batch of tasks for execution.
// The addition is performed in a goroutine for asynchronous delivery and returns immediately.
// Returns the number of tasks submitted (i.e., len(tasks)) and no error.
func (s *channelStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for _, task := range tasks {
			select {
			case s.taskChans[s.next(task)] <- task:
			case <-s.quit:
				return
			}
		}
	}()
	return len(tasks), nil
}

// Shutdown gracefully shuts down the channel strategy:
// - Signals all SubmitBatch goroutines to stop early.
// - Waits for all SubmitBatch goroutines to finish.
// - Closes all worker task channels, signaling workers to exit.
func (s *channelStrategy[T, R]) Shutdown() {
	close(s.quit) // Signal all SubmitBatch goroutines to stop
	s.wg.Wait()   // Wait for all SubmitBatch goroutines to finish
	for _, ch := range s.taskChans {
		close(ch)
	}
}

// Worker runs the worker event loop for the specified workerID.
// It receives tasks from its dedicated channel and executes them.
// Properly drains remaining tasks on context cancellation to ensure all submitted tasks are processed.
func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	for {
		select {
		case <-ctx.Done():
			s.drain(ctx, executor, h, workerID)
			return ctx.Err()
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return nil
			}
			err := executeSubmitted(ctx, t, s.config, executor, h)
			if err := handleExecutionError(err, s.config.ContinueOnErr, func() {
				s.drain(ctx, executor, h, workerID)
			}); err != nil {
				return err
			}
		}
	}
}

// drain processes any remaining tasks in the channel during context cancellation.
//
// It drains the worker's channel, executing all tasks until channel is empty or closed.
// Ensures all submitted tasks are processed even when context is cancelled.
func (s *channelStrategy[T, R]) drain(
	ctx context.Context,
	executor types.ProcessFunc[T, R],
	h types.ResultHandler[T, R],
	workerID int64,
) {
	for {
		select {
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return
			}
			_ = executeSubmitted(ctx, t, s.config, executor, h)
		default:
			return
		}
	}
}

// next returns the next channel index in a round-robin fashion using an atomic counter.
func (s *channelStrategy[T, R]) next(t *types.SubmittedTask[T, R]) int64 {
	if s.affinityFunc != nil {
		key := s.affinityFunc(t.Task)
		hash := s.fnvHash(key)
		return int64(hash % uint32(len(s.taskChans)))
	}
	return s.counter.Add(1) % int64(len(s.taskChans))
}

// fnvHash computes the FNV-1a hash for the given string key.
//
// FNV-1a (Fowler–Noll–Vo) is a fast, non-cryptographic hash function
// often used for hash tables. This function is used to spread
// affinity keys uniformly across internal task channels.
//
// https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function
func (s *channelStrategy[T, R]) fnvHash(key string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)

	hash := uint32(offset32)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime32
	}
	return hash
}
