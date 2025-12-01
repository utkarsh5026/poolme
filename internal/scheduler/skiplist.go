package scheduler

import (
	"context"
	"math/bits"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	defaultMaxLevel = 16
)

// slNode uses preallocated arrays and atomic operations
type slNode[T, R any] struct {
	tasks        []*types.SubmittedTask[T, R]
	nextAtLevel  [defaultMaxLevel]*slNode[T, R] // Array instead of slice
	mu           sync.Mutex
	priorityTask T
	level        int // Track actual level for this node
}

// skipList uses multiple optimization techniques:
// - Object pooling for update arrays
// - Fast random level generation
// - Preallocated node arrays
// - Atomic operations for lock-free reads
type skipList[T, R any] struct {
	head       *slNode[T, R]
	mu         sync.Mutex
	level      int32 // Use int32 for atomic operations
	lessFunc   func(a, b T) bool
	size       atomic.Int64
	updatePool sync.Pool // Pool for update arrays
}

func newSkipList[T, R any](lessFunc func(a, b T) bool) *skipList[T, R] {
	sl := &skipList[T, R]{
		head:     &slNode[T, R]{},
		lessFunc: lessFunc,
	}
	atomic.StoreInt32(&sl.level, 1)

	sl.updatePool = sync.Pool{
		New: func() any {
			arr := make([]*slNode[T, R], defaultMaxLevel)
			return &arr
		},
	}

	return sl
}

func (sl *skipList[T, R]) Pop() *types.SubmittedTask[T, R] {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	for {
		first := sl.head.nextAtLevel[0]
		if first == nil {
			return nil
		}

		first.mu.Lock()
		if len(first.tasks) == 0 {
			sl.removeNodeUnsafe(first)
			first.mu.Unlock()
			continue
		}

		task := first.tasks[0]
		first.tasks = first.tasks[1:]
		sl.size.Add(-1)

		if len(first.tasks) == 0 {
			sl.removeNodeUnsafe(first)
		}

		first.mu.Unlock()
		return task
	}
}

func (sl *skipList[T, R]) Push(task *types.SubmittedTask[T, R]) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	updatePtr, ok := sl.updatePool.Get().(*[]*slNode[T, R])
	if !ok {
		panic("skiplist: invalid type from updatePool")
	}
	update := *updatePtr
	defer func() {
		for i := range update {
			update[i] = nil
		}
		sl.updatePool.Put(updatePtr)
	}()

	curr := sl.head
	currentLevel := int(atomic.LoadInt32(&sl.level))

	// Single traversal using cached priority values
	for l := currentLevel - 1; l >= 0; l-- {
		next := curr.nextAtLevel[l]
		for next != nil {
			// Lock-free read of cached priority
			if sl.lessFunc(next.priorityTask, task.Task) {
				curr = next
				next = curr.nextAtLevel[l]
			} else {
				break
			}
		}
		update[l] = curr
	}

	// Check for same priority node
	candidate := update[0].nextAtLevel[0]
	if candidate != nil && !sl.lessFunc(task.Task, candidate.priorityTask) &&
		!sl.lessFunc(candidate.priorityTask, task.Task) {
		candidate.mu.Lock()
		candidate.tasks = append(candidate.tasks, task)
		candidate.mu.Unlock()
		sl.size.Add(1)
		return
	}

	// Create new node with random level
	newLevel := sl.randomLevel()
	if newLevel > currentLevel {
		for i := currentLevel; i < newLevel; i++ {
			update[i] = sl.head
		}
		atomic.StoreInt32(&sl.level, int32(newLevel))
	}

	newNode := &slNode[T, R]{
		tasks:        []*types.SubmittedTask[T, R]{task},
		priorityTask: task.Task,
		level:        newLevel,
	}

	// Insert node at all levels
	for i := range newLevel {
		newNode.nextAtLevel[i] = update[i].nextAtLevel[i]
		update[i].nextAtLevel[i] = newNode
	}

	sl.size.Add(1)
}

func (sl *skipList[T, R]) Len() int {
	return int(sl.size.Load())
}

func (sl *skipList[T, R]) removeNodeUnsafe(node *slNode[T, R]) {
	updatePtr, ok := sl.updatePool.Get().(*[]*slNode[T, R])
	if !ok {
		panic("skiplist: invalid type from updatePool")
	}
	update := *updatePtr
	defer func() {
		for i := range update {
			update[i] = nil
		}
		sl.updatePool.Put(updatePtr)
	}()

	currentLevel := int(atomic.LoadInt32(&sl.level))
	for l := currentLevel - 1; l >= 0; l-- {
		curr := sl.head
		for curr.nextAtLevel[l] != nil && curr.nextAtLevel[l] != node {
			curr = curr.nextAtLevel[l]
		}
		update[l] = curr
	}

	for l := 0; l < currentLevel; l++ {
		if update[l].nextAtLevel[l] == node {
			update[l].nextAtLevel[l] = node.nextAtLevel[l]
		}
	}

	// Reduce level if needed
	for currentLevel > 1 && sl.head.nextAtLevel[currentLevel-1] == nil {
		currentLevel--
	}
	atomic.StoreInt32(&sl.level, int32(currentLevel))
}

// slStrategy uses the skiplist
type slStrategy[T any, R any] struct {
	sl            *skipList[T, R]
	conf          *ProcessorConfig[T, R]
	availableChan chan struct{}
}

func newSlStrategy[T any, R any](conf *ProcessorConfig[T, R], tasks []*types.SubmittedTask[T, R]) *slStrategy[T, R] {
	sl := newSkipList[T, R](conf.LessFunc)

	for _, task := range tasks {
		sl.Push(task)
	}

	return &slStrategy[T, R]{
		sl:            sl,
		conf:          conf,
		availableChan: make(chan struct{}, conf.TaskBuffer),
	}
}

func (s *slStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	s.sl.Push(task)

	select {
	case s.availableChan <- struct{}{}:
	default:
	}

	return nil
}

func (s *slStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	for _, task := range tasks {
		s.sl.Push(task)
	}

	for range tasks {
		select {
		case s.availableChan <- struct{}{}:
		default:
		}
	}

	return len(tasks), nil
}

func (s *slStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	drainFunc := func() {
		s.drain(ctx, executor, h)
	}

	for {
		select {
		case <-ctx.Done():
			drainFunc()
			return ctx.Err()

		case _, ok := <-s.availableChan:
			if !ok {
				return nil
			}

			for {
				task := s.sl.Pop()
				if task == nil {
					break
				}
				if err := handleWithCare(ctx, task, s.conf, executor, h, drainFunc); err != nil {
					return err
				}
			}
		}
	}
}

func (s *slStrategy[T, R]) Shutdown() {
	close(s.availableChan)
}

func (s *slStrategy[T, R]) drain(ctx context.Context, f types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	for {
		task := s.sl.Pop()
		if task == nil {
			return
		}
		_ = executeSubmitted(ctx, task, s.conf, f, h)
	}
}

// randomLevel generates a random level using bit manipulation
// Much faster than calling rand.Float64() multiple times
func (sl *skipList[T, R]) randomLevel() int {
	random := rand.Uint64()
	level := bits.TrailingZeros64(random) + 1
	if level > defaultMaxLevel {
		return defaultMaxLevel
	}
	return level
}
