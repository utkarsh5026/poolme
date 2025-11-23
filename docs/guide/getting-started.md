# Getting Started

Get up and running with PoolMe in minutes.

## Installation

Install PoolMe using `go get`:

```bash
go get github.com/utkarsh5026/poolme/pool
```

## Requirements

- Go 1.18 or higher (for generics support)
- `golang.org/x/time/rate` (automatically installed)

## Your First Worker Pool

Let's create a simple worker pool that processes integers and returns strings:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // Create a pool with 5 workers
    p := pool.NewWorkerPool[int, string](
        pool.WithWorkerCount(5),
    )

    // Define your tasks
    tasks := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

    // Process tasks concurrently
    results, err := p.Process(
        context.Background(),
        tasks,
        func(ctx context.Context, n int) (string, error) {
            // Simulate some work
            return fmt.Sprintf("Processed: %d", n*n), nil
        },
    )

    if err != nil {
        log.Fatal(err)
    }

    // Print results
    for i, result := range results {
        fmt.Printf("Task %d: %s\n", i+1, result)
    }
}
```

## Understanding the Code

Let's break down what's happening:

### 1. Create a Pool

```go
p := pool.NewWorkerPool[int, string](
    pool.WithWorkerCount(5),
)
```

- `[int, string]` specifies the generic types (input: `int`, output: `string`)
- `WithWorkerCount(5)` creates 5 concurrent workers
- More configuration options available (buffer size, retries, rate limiting, etc.)

### 2. Define Your Processing Function

```go
func(ctx context.Context, n int) (string, error) {
    return fmt.Sprintf("Processed: %d", n*n), nil
}
```

This function:
- Takes a `context.Context` for cancellation support
- Takes your input type (`int` in this case)
- Returns your output type (`string`) and an `error`

### 3. Process Tasks

```go
results, err := p.Process(context.Background(), tasks, processFn)
```

- `Process` blocks until all tasks complete or an error occurs
- Results are returned in the same order as input tasks
- Returns the first error encountered (unless `continueOnError` is enabled)

## Next Steps

Now that you've created your first worker pool, explore:

- **[Core Concepts](/guide/core-concepts)** - Understand processing modes and patterns
- **[Configuration Options](/api/options)** - Learn about all available options
- **[Examples](/examples/basic)** - See real-world usage patterns

## Common Configuration

Here are some commonly used configurations:

### With Retry Logic

```go
p := pool.NewWorkerPool[Task, Result](
    pool.WithWorkerCount(10),
    pool.WithMaxAttempts(3),
    pool.WithInitialDelay(100 * time.Millisecond),
)
```

### With Rate Limiting

```go
limiter := rate.NewLimiter(rate.Limit(100), 10) // 100 req/sec, burst of 10
p := pool.NewWorkerPool[Task, Result](
    pool.WithRateLimiter(limiter),
)
```

### With Error Continuation

```go
p := pool.NewWorkerPool[Task, Result](
    pool.WithContinueOnError(true), // Don't stop on first error
)
```

### With Lifecycle Hooks

```go
p := pool.NewWorkerPool[Task, Result](
    pool.WithBeforeTaskStart(func(task Task) {
        log.Printf("Starting task: %v", task)
    }),
    pool.WithOnTaskEnd(func(task Task, result Result, err error) {
        log.Printf("Task completed: %v, error: %v", task, err)
    }),
)
```

## Troubleshooting

### Issue: "Too many open files"

Reduce worker count or add rate limiting:

```go
pool.WithWorkerCount(runtime.NumCPU())
```

### Issue: Tasks timing out

Add context with timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
results, err := p.Process(ctx, tasks, processFn)
```

### Issue: Memory usage too high

Reduce task buffer size:

```go
pool.WithTaskBuffer(10) // Small buffer
```
