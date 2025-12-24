package scheduler

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	retryForSameChannel = 4

	// FNV-1a hash parameters
	offset32 = 2166136261
	prime32  = 16777619
)

// channelStrategy provides a simple scheduling strategy that distributes tasks
// to a set of worker-specific channels in a round-robin fashion.
//
// Each worker goroutine reads from its own channel to execute submitted tasks.
type channelStrategy[T any, R any] struct {
	config       *ProcessorConfig[T, R]            // Processor configuration parameters.
	taskChans    []chan *types.SubmittedTask[T, R] // Per-worker task channels.
	counter      atomic.Int64                      // Atomic counter for round-robin channel selection.
	quitter      *workerSignal                     // Quitter to signal shutdown.
	affRouter    *affinityRouter[T, R]             // Affinity router for consistent task routing.
	submitWg     sync.WaitGroup                    // WaitGroup to track in-flight submissions
	submitMu     sync.Mutex                        // Mutex to protect submission during shutdown
	shutdownOnce sync.Once                         // Ensures shutdown is called only once
	runner       *workerRunner[T, R]
}

// newChannelStrategy creates a new instance of channelStrategy.
// It initializes the required number of task channels (one per worker),
// using the maximum of runtime.NumCPU() and configured WorkerCount.
func newChannelStrategy[T any, R any](conf *ProcessorConfig[T, R]) *channelStrategy[T, R] {
	n := max(1, conf.WorkerCount)
	c := &channelStrategy[T, R]{
		config:    conf,
		taskChans: make([]chan *types.SubmittedTask[T, R], n),
		quitter:   newWorkerSignal(),
	}

	if conf.AffinityFunc != nil {
		c.affRouter = &affinityRouter[T, R]{
			affinityFunc: conf.AffinityFunc,
			workerCount:  n,
		}
	}

	for i := range n {
		c.taskChans[i] = make(chan *types.SubmittedTask[T, R], conf.TaskBuffer)
	}

	c.runner = newWorkerRunner(conf, c)
	return c
}

// Submit submits a single task to the next worker channel using round-robin scheduling.
// Returns nil if successful, or an error if the scheduler is shutting down.
func (s *channelStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	s.submitMu.Lock()
	select {
	case <-s.quitter.Wait():
		s.submitMu.Unlock()
		return ErrSchedulerClosed
	default:
	}
	s.submitWg.Add(1)
	s.submitMu.Unlock()

	defer s.submitWg.Done()

	if exit := s.tryToSubmit(s.next(task), task); exit {
		return ErrSchedulerClosed
	}
	return nil
}

// SubmitBatch submits a batch of tasks for execution synchronously.
// Returns the number of tasks successfully submitted and an error if shutdown was signaled during submission.
// If shutdown occurs mid-batch, returns the count of tasks submitted before shutdown and ErrSchedulerClosed error.
func (s *channelStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	s.submitMu.Lock()
	select {
	case <-s.quitter.Wait():
		s.submitMu.Unlock()
		return 0, ErrSchedulerClosed
	default:
	}
	s.submitWg.Add(1)
	s.submitMu.Unlock()

	defer s.submitWg.Done()

	submitted := 0
	for _, task := range tasks {
		idx := s.next(task)
		if exit := s.tryToSubmit(idx, task); exit {
			return submitted, ErrSchedulerClosed
		}
		submitted++
	}
	return submitted, nil
}

// tryToSubmit quickly enqueues a task to a worker channel:
//  1. Attempts a few non-blocking sends to idx via sendToChannel.
//  2. If not sent, tries other channels round-robin.
//  3. As a last resort, blocks on idx or exits early if quitting.
//
// Returns true if quit signal received, otherwise false.
func (s *channelStrategy[T, R]) tryToSubmit(idx int64, task *types.SubmittedTask[T, R]) (exit bool) {
	sent, exit := s.sendToChannel(idx, task)
	if exit {
		return true
	}
	if sent {
		return false
	}

	if sent = s.submitRoundRobin(int(idx), task); !sent {
		select {
		case s.taskChans[idx] <- task:
		case <-s.quitter.Wait():
			return true
		}
	}
	return false
}

// sendToChannel tries to send a task to the channel at index idx.
// It performs a small number of tries (controlled by retryForSameChannel) to put the task with a non-blocking attempt.
// Returns (true, false) if successfully sent, (true, true) if quit signal received, or (false, false) if not sent after retries.
func (s *channelStrategy[T, R]) sendToChannel(idx int64, task *types.SubmittedTask[T, R]) (sent bool, exit bool) {
	for range retryForSameChannel {
		select {
		case s.taskChans[idx] <- task:
			return true, false
		case <-s.quitter.Wait():
			return true, true
		default:
		}
	}
	return false, false
}

// submitRoundRobin attempts to enqueue the given task to a different worker's channel
// (not at idx), checking channels in round-robin order. This helps balance load if the main
// channel is full. Returns true if the task is submitted, or true immediately if a quit signal was received.
func (s *channelStrategy[T, R]) submitRoundRobin(idx int, task *types.SubmittedTask[T, R]) bool {
	numWorkers := len(s.taskChans)
	for j := range numWorkers - 1 {
		altIdx := (idx + j + 1) % numWorkers
		select {
		case s.taskChans[altIdx] <- task:
			return true
		case <-s.quitter.Wait():
			return true
		default:
			continue
		}
	}
	return false
}

// Shutdown gracefully shuts down the channel strategy:
// - Signals ongoing task submissions to stop.
// - Waits for all in-flight submissions to complete.
// - Closes all worker task channels, signaling workers to exit.
func (s *channelStrategy[T, R]) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.submitMu.Lock()
		s.quitter.Close() // Signal ongoing submissions to stop
		s.submitMu.Unlock()

		s.submitWg.Wait() // Wait for all in-flight submissions to complete

		for _, ch := range s.taskChans {
			close(ch)
		}
	})
}

// Worker runs the worker event loop for the specified workerID.
// It receives tasks from its dedicated channel and executes them.
// Properly drains remaining tasks on context cancellation to ensure all submitted tasks are processed.
func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	drain := func() {
		s.drain(ctx, executor, h, workerID)
	}
	for {
		select {
		case <-ctx.Done():
			drain()
			return ctx.Err()
		case <-s.quitter.Wait():
			drain()
			return nil
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return nil
			}
			if err := s.runner.Execute(ctx, t, executor, h, drain); err != nil {
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
	drained := 0
	for {
		select {
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return
			}
			s.runner.ExecuteWithoutCare(ctx, t, executor, h)
			drained++
		default:
			return
		}
	}
}

// next returns the next channel index in a round-robin fashion using an atomic counter.
func (s *channelStrategy[T, R]) next(t *types.SubmittedTask[T, R]) int64 {
	// #nosec G115 -- len(s.taskChans) is bounded by max(NumCPU, WorkerCount), always << math.MaxUint32
	n := uint32(len(s.taskChans))
	if s.affRouter != nil {
		return s.affRouter.Route(t.Task)
	}
	return s.counter.Add(1) % int64(n)
}

// affinityRouter routes tasks to workers based on an affinity key.
type affinityRouter[T any, R any] struct {
	affinityFunc func(t T) string
	workerCount  int
}

// fnvHash computes the FNV-1a hash for the given string key.
//
// FNV-1a (Fowler–Noll–Vo) is a fast, non-cryptographic hash function
// often used for hash tables. This function is used to spread
// affinity keys uniformly across internal task channels.
//
// https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function
func (a *affinityRouter[T, R]) fnvHash(key string) uint32 {
	hash := uint32(offset32)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime32
	}
	return hash
}

// Route computes the worker index for the given task based on its affinity key.
// It uses FNV-1a hashing to consistently map tasks with the same affinity key
func (a *affinityRouter[T, R]) Route(t T) int64 {
	key := a.affinityFunc(t)
	hash := a.fnvHash(key)
	return int64(hash % uint32(a.workerCount)) // #nosec G115 -- workerCount is bounded, modulo result fits in uint32 and int64
}
