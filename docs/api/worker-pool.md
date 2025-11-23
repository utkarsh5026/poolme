# WorkerPool API Reference

Complete API documentation for the `WorkerPool` type.

## Type Definition

```go
type WorkerPool[T any, R any] struct {
    // ... internal fields
}
```

**Type Parameters:**
- `T`: Input task type
- `R`: Result type

---

## Constructor

### NewWorkerPool

Creates a new worker pool with the given options.

```go
func NewWorkerPool[T any, R any](opts ...WorkerPoolOption) *WorkerPool[T, R]
```

**Parameters:**
- `opts`: Variable number of configuration options

**Returns:**
- Configured `*WorkerPool[T, R]` instance

**Default Configuration:**
- `workerCount`: `runtime.GOMAXPROCS(0)` (number of logical CPUs)
- `taskBuffer`: equal to `workerCount`
- `maxAttempts`: 1 (no retries)
- `initialDelay`: 0 (no delay)
- `continueOnError`: false

**Example:**

```go
pool := pool.NewWorkerPool[int, string](
    pool.WithWorkerCount(10),
    pool.WithTaskBuffer(20),
    pool.WithMaxAttempts(3),
)
```

---

## Batch Processing Methods

### Process

Executes a batch of tasks concurrently and returns results in order.

```go
func (wp *WorkerPool[T, R]) Process(
    ctx context.Context,
    tasks []T,
    processFn ProcessFunc[T, R],
) ([]R, error)
```

**Parameters:**
- `ctx`: Context for cancellation and timeout control
- `tasks`: Slice of tasks to process
- `processFn`: Function to process each task `func(context.Context, T) (R, error)`

**Returns:**
- `results`: Slice of results in the same order as input tasks
- `error`: First error encountered, or nil if all succeeded

**Behavior:**
- Uses `min(workerCount, len(tasks))` workers
- Results maintain input order
- Blocks until all tasks complete
- Returns first error unless `continueOnError` is enabled

**Example:**

```go
tasks := []int{1, 2, 3, 4, 5}
results, err := pool.Process(ctx, tasks,
    func(ctx context.Context, n int) (string, error) {
        return fmt.Sprintf("processed %d", n), nil
    },
)
```

---

### ProcessMap

Executes tasks from a map concurrently and returns a result map.

```go
func (wp *WorkerPool[T, R]) ProcessMap(
    ctx context.Context,
    tasks map[string]T,
    processFn ProcessFunc[T, R],
) (map[string]R, error)
```

**Parameters:**
- `ctx`: Context for cancellation and timeout control
- `tasks`: Map of tasks with string keys
- `processFn`: Function to process each task

**Returns:**
- `results`: Map of results with same keys as input
- `error`: First error encountered, or nil

**Example:**

```go
tasks := map[string]int{"a": 1, "b": 2, "c": 3}
results, err := pool.ProcessMap(ctx, tasks, processFunc)
// results["a"] = ..., results["b"] = ..., etc.
```

---

### ProcessStream

Processes tasks from a channel and returns result/error channels.

```go
func (wp *WorkerPool[T, R]) ProcessStream(
    ctx context.Context,
    taskChan <-chan T,
    processFn ProcessFunc[T, R],
) (resultChan <-chan R, errChan <-chan error)
```

**Parameters:**
- `ctx`: Context for cancellation
- `taskChan`: Input channel (caller must close)
- `processFn`: Function to process each task

**Returns:**
- `resultChan`: Output channel for results (auto-closed)
- `errChan`: Channel for errors

**Important:**
- Caller MUST close `taskChan` when done sending tasks
- Results may arrive in any order (not preserved)
- Suitable for streaming/pipeline scenarios

**Example:**

```go
taskChan := make(chan int, 10)
go func() {
    for i := 0; i < 100; i++ {
        taskChan <- i
    }
    close(taskChan)
}()

resultChan, errChan := pool.ProcessStream(ctx, taskChan, processFunc)
for result := range resultChan {
    fmt.Println(result)
}
```

---

## Long-Running Pool Methods

### Start

Initializes the pool in long-running mode with persistent workers.

```go
func (wp *WorkerPool[T, R]) Start(
    ctx context.Context,
    processFn ProcessFunc[T, R],
) error
```

**Parameters:**
- `ctx`: Context for pool lifetime
- `processFn`: Function to process all submitted tasks

**Returns:**
- `error`: Non-nil if pool already started

**Behavior:**
- Spawns `workerCount` persistent goroutines
- Workers remain active until `Shutdown`
- All `Submit` calls use this `processFn`

**Example:**

```go
err := pool.Start(ctx, func(ctx context.Context, n int) (string, error) {
    return fmt.Sprintf("result: %d", n*2), nil
})
if err != nil {
    log.Fatal(err)
}
defer pool.Shutdown(5 * time.Second)
```

---

### Submit

Submits a single task for asynchronous processing.

```go
func (wp *WorkerPool[T, R]) Submit(task T) (*Future[R, int64], error)
```

**Parameters:**
- `task`: The task to process

**Returns:**
- `future`: Future for retrieving the result
- `error`: Non-nil if pool not started or shut down

**Requirements:**
- Pool must be started with `Start()`
- Returns error if pool shut down

**Example:**

```go
future, err := pool.Submit(42)
if err != nil {
    log.Fatal(err)
}

// Option 1: Block for result
result, err := future.Get()

// Option 2: Wait with timeout
result, err := future.GetWithTimeout(5 * time.Second)

// Option 3: Check without blocking
if future.IsReady() {
    result, _ := future.Get()
}
```

---

### Shutdown

Gracefully shuts down the worker pool.

```go
func (wp *WorkerPool[T, R]) Shutdown(timeout time.Duration) error
```

**Parameters:**
- `timeout`: Max wait duration (0 = wait forever)

**Returns:**
- `error`: Non-nil if not started, already shut down, or timeout exceeded

**Shutdown Process:**
1. Mark pool as shutdown (reject new submits)
2. Close task channel (signal workers)
3. Wait for workers to finish
4. Cancel context

**Example:**

```go
// Graceful with timeout
if err := pool.Shutdown(5 * time.Second); err != nil {
    log.Printf("shutdown error: %v", err)
}

// Wait indefinitely
pool.Shutdown(0)

// Common defer pattern
defer pool.Shutdown(10 * time.Second)
```

---

## Type Definitions

### ProcessFunc

Function type for processing tasks.

```go
type ProcessFunc[T any, R any] func(context.Context, T) (R, error)
```

**Parameters:**
- `context.Context`: For cancellation/timeout
- `T`: Input task

**Returns:**
- `R`: Result value
- `error`: Processing error, if any

---

## Thread Safety

All public methods of `WorkerPool` are **thread-safe** and can be called concurrently from multiple goroutines:

✅ **Safe to call concurrently:**
- `Process()`
- `ProcessMap()`
- `ProcessStream()`
- `Submit()` (after `Start()`)
- `Shutdown()`

⚠️ **Not safe:**
- Calling `Start()` multiple times concurrently
- Calling `Shutdown()` multiple times concurrently

---

## Performance Notes

- **Worker Count**: Optimal value is usually `runtime.NumCPU()` for CPU-bound tasks, higher for I/O-bound
- **Buffer Size**: Larger buffers reduce contention but increase memory
- **Context Overhead**: Minimal; always use context for proper cancellation
- **Memory**: `O(workerCount + bufferSize + len(tasks))`
- **Goroutines**: Exactly `workerCount` goroutines created

---

## See Also

- [Options API](/api/options) - Configuration options
- [Future API](/api/future) - Async result handling
- [Examples](/examples/basic) - Usage examples
