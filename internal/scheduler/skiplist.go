package scheduler

import (
	"context"
	"math/bits"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	"github.com/utkarsh5026/gopool/internal/types"
)

const (
	// defaultMaxLevel defines the maximum height of the skip list.
	// A value of 16 provides good performance for up to ~65,536 elements
	// while keeping memory overhead reasonable.
	defaultMaxLevel = 16
)

// slNode represents a node in the skip list structure.
type slNode[T, R any] struct {
	// tasks holds all submitted tasks with the same priority.
	// Tasks are stored in submission order (FIFO within same priority).
	tasks []*types.SubmittedTask[T, R]

	// nextAtLevel contains forward pointers for each level.
	// Level 0 points to the next node in sorted order,
	// higher levels provide express lanes for faster search.
	nextAtLevel [defaultMaxLevel]*slNode[T, R]

	// mu protects concurrent access to the tasks slice.
	mu sync.Mutex

	// priorityTask caches the priority value for fast comparison.
	// This avoids locking and repeated extraction of priority from tasks.
	priorityTask T

	// level indicates the actual height of this node.
	// Nodes with higher levels participate in more express lanes.
	level int
}

// newSlNode creates a new skip list node with the given task and level.
func newSlNode[T, R any](task *types.SubmittedTask[T, R], level int) *slNode[T, R] {
	return &slNode[T, R]{
		tasks:        []*types.SubmittedTask[T, R]{task},
		priorityTask: task.Task,
		level:        level,
	}
}

// skipList implements a lock-based concurrent skip list for priority queue operations.
//
// A skip list is a probabilistic data structure that provides O(log n) average time
// complexity for search, insertion, and deletion operations. It maintains multiple
// levels of linked lists, where each level acts as an "express lane" for faster traversal.
type skipList[T, R any] struct {
	// head is a sentinel node that simplifies boundary conditions.
	// It has no tasks and always exists at the maximum level.
	head *slNode[T, R]

	// mu is the global lock protecting structural modifications.
	// Acquired during Push (node insertion) and removeNodeUnsafe.
	mu sync.Mutex

	// level tracks the current maximum level of the skip list.
	// Stored as int32 for atomic reads without locking.
	level int32

	// lessFunc defines the priority comparison function.
	// Returns true if a has higher priority than b (should come first).
	lessFunc func(a, b T) bool

	// size tracks the total number of tasks across all nodes.
	// Updated atomically for lock-free reads.
	size atomic.Int64

	// updatePool reuses update arrays during traversal.
	// Each operation needs an array to track predecessor nodes at each level.
	updatePool sync.Pool
}

// newSkipList creates and initializes a new skip list with the given priority comparison function.
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

// Pop removes and returns the highest priority task from the skip list.
//
// This operation:
//  1. Acquires the global lock
//  2. Retrieves the first node (highest priority)
//  3. Locks the node and pops the first task
//  4. Removes the node if it becomes empty
//  5. Continues to the next node if the current one is empty
//
// Returns nil if the skip list is empty.
//
// Time complexity: O(1) amortized, O(log n) worst case (when removing empty nodes).
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

// Push inserts a task into the skip list according to its priority.
//
// The operation follows these steps:
//  1. Traverse from the top level down to find the insertion point
//  2. If a node with the same priority exists, append to that node's task list
//  3. Otherwise, create a new node with a randomly generated level
//  4. Insert the node at all levels from 0 to its assigned level
//
// Tasks with identical priority maintain FIFO order within their shared node.
//
// Time complexity: O(log n) average, O(n) worst case.
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
			if sl.lessFunc(next.priorityTask, task.Task) {
				curr = next
				next = curr.nextAtLevel[l]
				continue
			}
			break
		}
		update[l] = curr
	}

	candidate := update[0].nextAtLevel[0]
	if sl.checkForSamePriority(candidate, task) {
		return
	}

	sl.insertUnsafe(currentLevel, task, update)
}

// checkForSamePriority checks if the given node has the same priority as the task.
//
// If they are equal, the task is appended to the node's task list.
//
// Returns true if the task was added to an existing node, false otherwise.
func (sl *skipList[T, R]) checkForSamePriority(n *slNode[T, R], t *types.SubmittedTask[T, R]) bool {
	if n == nil {
		return false
	}

	equals := !sl.lessFunc(t.Task, n.priorityTask) && !sl.lessFunc(n.priorityTask, t.Task)
	if equals {
		n.mu.Lock()
		n.tasks = append(n.tasks, t)
		n.mu.Unlock()
		sl.size.Add(1)
		return true
	}

	return false
}

// insertUnsafe inserts a new node into the skip list at the appropriate levels.
//
// IMPORTANT: This method must be called while holding sl.mu.
// The "Unsafe" suffix indicates it's not thread-safe on its own.
//
// The operation:
//  1. Generates a random level for the new node
//  2. Updates the skip list's level if the new node's level is higher
//  3. Inserts the new node at all levels from 0 to its assigned level
//
// This is typically called when no existing node has the same priority.
func (sl *skipList[T, R]) insertUnsafe(currentLevel int, task *types.SubmittedTask[T, R], update []*slNode[T, R]) {
	newLevel := sl.randomLevel()
	if newLevel > currentLevel {
		for i := currentLevel; i < newLevel; i++ {
			update[i] = sl.head
		}
		atomic.StoreInt32(&sl.level, int32(newLevel)) // #nosec G115 -- newLevel is capped at defaultMaxLevel (32), safe for int32
	}

	newNode := newSlNode(task, newLevel)

	// Insert node at all levels
	for i := range newLevel {
		newNode.nextAtLevel[i] = update[i].nextAtLevel[i]
		update[i].nextAtLevel[i] = newNode
	}

	sl.size.Add(1)
}

// Len returns the total number of tasks currently in the skip list.
// This is a lock-free operation using atomic load.
func (sl *skipList[T, R]) Len() int {
	return int(sl.size.Load())
}

// removeNodeUnsafe removes a node from the skip list at all levels.
//
// IMPORTANT: This method must be called while holding sl.mu.
// The "Unsafe" suffix indicates it's not thread-safe on its own.
//
// The operation:
//  1. Traverses each level to find predecessors of the target node
//  2. Updates predecessor pointers to bypass the node
//  3. Reduces the skip list level if the top levels become empty
//
// This is typically called when a node's task list becomes empty.
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
	atomic.StoreInt32(&sl.level, int32(currentLevel)) // #nosec G115 -- currentLevel is capped at defaultMaxLevel (32), safe for int32
}

// slStrategy implements the Strategy interface using a skip list for priority-based scheduling.
//
// This strategy provides:
//   - Priority-based task ordering via the skip list
//   - O(log n) task insertion performance
//   - O(1) amortized task extraction (highest priority first)
type slStrategy[T any, R any] struct {
	// sl is the underlying skip list storing tasks by priority.
	sl *skipList[T, R]

	// conf holds the processor configuration including the priority comparison function.
	conf *ProcessorConfig[T, R]

	// available signals workers when tasks are available.
	// Non-blocking signals prevent submission from blocking when workers are busy.
	available *workerSignal

	// runner manages worker execution loops.
	runner *workerRunner[T, R]
}

// newSlStrategy creates a new skip list-based scheduling strategy.
//
// Parameters:
//   - conf: Configuration including the priority comparison function
//   - tasks: Initial tasks to populate the skip list (can be empty)
//
// The strategy is initialized with a buffered channel sized according to TaskBuffer
// to minimize blocking during task submission.
func newSlStrategy[T any, R any](conf *ProcessorConfig[T, R], tasks []*types.SubmittedTask[T, R]) *slStrategy[T, R] {
	sl := newSkipList[T, R](conf.LessFunc)

	for _, task := range tasks {
		sl.Push(task)
	}

	s := &slStrategy[T, R]{
		sl:        sl,
		conf:      conf,
		available: newWorkerSignal(),
	}

	s.runner = newWorkerRunner(conf, s)
	return s
}

// Submit adds a single task to the skip list and signals a worker.
//
// The task is inserted according to its priority, and a non-blocking signal
// is sent to wake a worker. If all workers are busy, the signal is dropped.
//
// Returns nil (no errors in skip list submission).
func (s *slStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	s.sl.Push(task)
	s.available.Signal()
	return nil
}

// SubmitBatch adds multiple tasks to the skip list efficiently.
//
// All tasks are inserted into the skip list, then signals are sent to wake workers.
// Non-blocking signals prevent submission from blocking when workers are busy.
//
// Returns the number of tasks submitted (always len(tasks)) and nil error.
func (s *slStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	for _, task := range tasks {
		s.sl.Push(task)
	}

	for range tasks {
		s.available.Signal()
	}

	return len(tasks), nil
}

// Worker implements the worker loop for processing tasks from the skip list.
//
// The worker:
//  1. Waits for a signal on available.Wait() indicating tasks are available
//  2. Pops tasks from the skip list until empty
//  3. Processes each task using the executor function
//  4. Drains remaining tasks on context cancellation or signal close
func (s *slStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	drainFunc := func() {
		s.drain(ctx, executor, h)
	}

	for {
		select {
		case <-ctx.Done():
			drainFunc()
			return ctx.Err()

		case _, ok := <-s.available.Wait():
			if !ok {
				return nil
			}

			for {
				task := s.sl.Pop()
				if task == nil {
					break
				}
				if err := s.runner.Execute(ctx, task, executor, h, drainFunc); err != nil {
					return err
				}
			}
		}
	}
}

// Shutdown closes the available signal, signaling all workers to drain and exit.
// After shutdown, no new tasks can be submitted.
func (s *slStrategy[T, R]) Shutdown() {
	s.available.Close()
}

// drain processes all remaining tasks in the skip list during shutdown.
//
// This ensures no tasks are lost when the pool stops. All tasks are executed
// regardless of context cancellation (though individual tasks may respect the context).
func (s *slStrategy[T, R]) drain(ctx context.Context, f types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	for {
		task := s.sl.Pop()
		if task == nil {
			return
		}
		s.runner.ExecuteWithoutCare(ctx, task, f, h)
	}
}

// randomLevel generates a random level for a new skip list node.
//
// This implementation uses bit manipulation for performance:
//  1. Generates a single random 64-bit integer
//  2. Counts trailing zeros (geometric distribution with p=0.5)
//  3. Caps at defaultMaxLevel
func (sl *skipList[T, R]) randomLevel() int {
	random := rand.Uint64() // #nosec G404 -- non-cryptographic use for skip list level distribution
	level := bits.TrailingZeros64(random) + 1
	if level > defaultMaxLevel {
		return defaultMaxLevel
	}
	return level
}
