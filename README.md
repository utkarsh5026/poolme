<div align="center">

<img src="docs/public/logo.svg" alt="PoolMe Logo" width="200"/>

<h1>PoolMe</h1>

<p align="center">
  <strong>A simple, powerful, and type-safe worker pool for Go</strong>
</p>

<p align="center">
  Production-ready worker pool implementation using Go generics<br/>
  Designed for efficient concurrent task processing with advanced features
</p>

<p align="center">
  <a href="https://github.com/utkarsh5026/poolme/actions/workflows/ci.yml">
    <img src="https://github.com/utkarsh5026/poolme/actions/workflows/ci.yml/badge.svg" alt="CI">
  </a>
  <a href="https://golang.org/">
    <img src="https://img.shields.io/badge/Go-%3E%3D%201.18-blue.svg" alt="Go Version">
  </a>
  <a href="https://pkg.go.dev/github.com/utkarsh5026/poolme">
    <img src="https://pkg.go.dev/badge/github.com/utkarsh5026/poolme.svg" alt="Go Reference">
  </a>
  <br/>
  <a href="https://goreportcard.com/report/github.com/utkarsh5026/poolme">
    <img src="https://goreportcard.com/badge/github.com/utkarsh5026/poolme" alt="Go Report Card">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/License-Apache%202.0-green.svg" alt="License">
  </a>
  <a href="https://github.com/utkarsh5026/poolme/releases/latest">
    <img src="https://img.shields.io/github/v/release/utkarsh5026/poolme" alt="Release">
  </a>
</p>

<p align="center">
  <a href="#-features">Features</a> •
  <a href="#-installation">Installation</a> •
  <a href="#-quick-start">Quick Start</a> •
  <a href="#-usage">Usage</a> •
  <a href="#-performance">Performance</a> •
  <a href="#-api-reference">API</a>
</p>

</div>

<br/>

## ✨ Features

<table>
  <tr>
    <td width="50%">

**🔧 Generic & Type-Safe**

- Works with any task type (`T`) and result type (`R`)
- Leverages Go 1.18+ generics for compile-time safety
- Zero interface{} conversions

**⚙️ Highly Configurable**

- Fine-tune worker count and buffer sizes
- Flexible processing modes (slice, map, stream)
- Customizable error handling strategies

    </td>
    <td width="50%">

**🎯 Production-Ready**

- Context-aware cancellation and timeouts
- Built-in panic recovery in workers
- Thread-safe lifecycle hooks

**🚀 Advanced Features**

- Configurable retry policies with exponential backoff
- Rate limiting for external API calls
- Real-time task monitoring and metrics

    </tr>
  </table>

<br/>

## 📦 Installation

```bash
go get github.com/utkarsh5026/poolme
```

**Requirements:** Go 1.18 or higher

<br/>

## 🚀 Quick Start

Get up and running in less than a minute:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    ctx := context.Background()
    tasks := []int{1, 2, 3, 4, 5, 6, 7, 8}

    // Create a pool with 4 workers
    p := pool.NewWorkerPool[int, string](pool.WithWorkerCount(4))

    // Define the processing function
    processFn := func(ctx context.Context, task int) (string, error) {
        time.Sleep(10 * time.Millisecond)
        return fmt.Sprintf("Processed task %d", task), nil
    }

    // Process tasks and get results in order
    results, err := p.Process(ctx, tasks, processFn)
    if err != nil {
        panic(err)
    }

    fmt.Println(results)
    // Output: [Processed task 1 Processed task 2 ... Processed task 8]
}
```

<br/>

## 💡 Why Choose PoolMe?

| Feature             | PoolMe                               | Traditional Approaches     |
| ------------------- | ------------------------------------ | -------------------------- |
| **Type Safety**     | ✅ Full generic support              | ❌ Interface{} hell        |
| **Retry Logic**     | ✅ Built-in with exponential backoff | ⚠️ Manual implementation   |
| **Rate Limiting**   | ✅ Native support                    | ⚠️ External library needed |
| **Lifecycle Hooks** | ✅ Thread-safe monitoring            | ❌ Not available           |
| **Context Support** | ✅ First-class citizen               | ⚠️ Varies                  |
| **Panic Recovery**  | ✅ Automatic per worker              | ⚠️ Manual handling         |

<br/>

## 📖 Usage

### Processing Modes

<details open>
<summary><b>1️⃣ Slice Processing (Ordered Results)</b></summary>

<br/>

Process a slice of tasks and get results in the same order:

```go
tasks := []int{1, 2, 3, 4, 5}
p := pool.NewWorkerPool[int, string](pool.WithWorkerCount(2))

results, err := p.Process(ctx, tasks, processFn)
```

</details>

<details>
<summary><b>2️⃣ Map Processing (Key-Value Pairs)</b></summary>

<br/>

Process map entries and get results mapped by keys:

```go
tasks := map[string]int{
    "task1": 1,
    "task2": 2,
    "task3": 3,
}
p := pool.NewWorkerPool[int, string](pool.WithWorkerCount(2))

results, err := p.ProcessMap(ctx, tasks, processFn)
// results: map[string]string
```

</details>

<details>
<summary><b>3️⃣ Stream Processing (Channel-Based)</b></summary>

<br/>

Process tasks from a channel as they arrive:

```go
taskChan := make(chan int, 10)
p := pool.NewWorkerPool[int, string](pool.WithWorkerCount(2))

// Send tasks to channel
go func() {
    for i := 1; i <= 5; i++ {
        taskChan <- i
    }
    close(taskChan)
}()

resultChan := p.ProcessStream(ctx, taskChan, processFn)
for result := range resultChan {
    if result.Err != nil {
        fmt.Printf("Error: %v\n", result.Err)
    } else {
        fmt.Printf("Result: %v\n", result.Value)
    }
}
```

</details>

<br/>

### ⚙️ Configuration Options

<details>
<summary><b>Worker Count Configuration</b></summary>

<br/>

Control the number of concurrent workers:

```go
p := pool.NewWorkerPool[int, string](
    pool.WithWorkerCount(10), // 10 concurrent workers
)
```

</details>

<details>
<summary><b>Task Buffer Management</b></summary>

<br/>

Set the buffer size for the internal task channel:

```go
p := pool.NewWorkerPool[int, string](
    pool.WithWorkerCount(4),
    pool.WithTaskBuffer(100), // Buffer up to 100 tasks
)
```

</details>

<details>
<summary><b>Retry Policy with Exponential Backoff</b></summary>

<br/>

Configure automatic retries:

```go
p := pool.NewWorkerPool[int, string](
    pool.WithRetryPolicy(
        3,                      // Max 3 attempts per task
        100*time.Millisecond,   // Initial delay of 100ms
    ),
)
// Retry delays: 100ms, 200ms, 400ms (exponential backoff)
```

</details>

<details>
<summary><b>Rate Limiting</b></summary>

<br/>

Control task throughput to prevent overwhelming external services:

```go
p := pool.NewWorkerPool[int, string](
    pool.WithRateLimit(
        10.0,  // 10 tasks per second
        5,     // Burst of up to 5 tasks
    ),
)
```

</details>

<br/>

### 🎣 Lifecycle Hooks

Monitor and react to task lifecycle events:

<details>
<summary><b>Before Task Start Hook</b></summary>

<br/>

Called before each task begins processing:

```go
p := pool.NewWorkerPool[int, string](
    pool.WithBeforeTaskStart(func(task int) {
        log.Printf("Starting task: %d", task)
    }),
)
```

</details>

<details>
<summary><b>On Task End Hook</b></summary>

<br/>

Called after each task completes (success or failure):

```go
p := pool.NewWorkerPool[int, string](
    pool.WithOnTaskEnd(func(task int, result string, err error) {
        if err != nil {
            log.Printf("Task %d failed: %v", task, err)
        } else {
            log.Printf("Task %d completed: %s", task, result)
        }
    }),
)
```

</details>

<details>
<summary><b>On Each Retry Attempt Hook</b></summary>

<br/>

Called after each retry attempt (requires retry policy):

```go
p := pool.NewWorkerPool[int, string](
    pool.WithRetryPolicy(3, 100*time.Millisecond),
    pool.WithOnEachAttempt(func(task int, attempt int, err error) {
        log.Printf("Task %d attempt %d failed: %v", task, attempt, err)
    }),
)
```

</details>

> **⚠️ Important:** All hooks must be thread-safe as they may be called concurrently by multiple workers.

<br/>

### 🔥 Complete Example

Combining multiple features for a robust task processing system:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/utkarsh5026/poolme/pool"
)

type APIRequest struct {
    ID   int
    URL  string
}

func main() {
    ctx := context.Background()

    // Configure pool with multiple features
    p := pool.NewWorkerPool[APIRequest, string](
        pool.WithWorkerCount(5),
        pool.WithTaskBuffer(50),
        pool.WithRetryPolicy(3, 200*time.Millisecond),
        pool.WithRateLimit(10.0, 5),
        pool.WithBeforeTaskStart(func(task APIRequest) {
            log.Printf("Processing request %d: %s", task.ID, task.URL)
        }),
        pool.WithOnTaskEnd(func(task APIRequest, result string, err error) {
            if err != nil {
                log.Printf("Request %d failed: %v", task.ID, err)
            }
        }),
        pool.WithOnEachAttempt(func(task APIRequest, attempt int, err error) {
            log.Printf("Request %d retry attempt %d: %v", task.ID, attempt, err)
        }),
    )

    // Create tasks
    tasks := []APIRequest{
        {ID: 1, URL: "https://api.example.com/data/1"},
        {ID: 2, URL: "https://api.example.com/data/2"},
        {ID: 3, URL: "https://api.example.com/data/3"},
    }

    // Process with timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    results, err := p.Process(ctx, tasks, func(ctx context.Context, req APIRequest) (string, error) {
        // Simulate API call
        time.Sleep(100 * time.Millisecond)
        return fmt.Sprintf("Data from %s", req.URL), nil
    })

    if err != nil {
        log.Fatal(err)
    }

    for i, result := range results {
        fmt.Printf("Result %d: %s\n", i+1, result)
    }
}
```

<br/>

## 📚 API Reference

### Core Types

#### `WorkerPool[T, R]`

The main worker pool type.

- `T`: Task type (input)
- `R`: Result type (output)

### Constructor

```go
func NewWorkerPool[T, R any](options ...WorkerPoolOption) *WorkerPool[T, R]
```

### Processing Methods

```go
// Process a slice of tasks, returns ordered results
func (wp *WorkerPool[T, R]) Process(
    ctx context.Context,
    tasks []T,
    processFn func(context.Context, T) (R, error),
) ([]R, error)

// Process a map of tasks, returns results mapped by keys
func (wp *WorkerPool[T, R]) ProcessMap(
    ctx context.Context,
    tasks map[K]T,
    processFn func(context.Context, T) (R, error),
) (map[K]R, error)

// Process tasks from a channel, returns result channel
func (wp *WorkerPool[T, R]) ProcessStream(
    ctx context.Context,
    taskChan <-chan T,
    processFn func(context.Context, T) (R, error),
) <-chan StreamResult[R]
```

### Configuration Options

| Option                                                         | Description                                       |
| -------------------------------------------------------------- | ------------------------------------------------- |
| `WithWorkerCount(count int)`                                   | Set number of concurrent workers                  |
| `WithTaskBuffer(size int)`                                     | Set task channel buffer size                      |
| `WithRetryPolicy(maxAttempts int, initialDelay time.Duration)` | Configure retry behavior with exponential backoff |
| `WithRateLimit(tasksPerSecond float64, burst int)`             | Set rate limiting for task processing             |
| `WithBeforeTaskStart[T](func(T))`                              | Hook called before task processing                |
| `WithOnTaskEnd[T, R](func(T, R, error))`                       | Hook called after task completion                 |
| `WithOnEachAttempt[T](func(T, int, error))`                    | Hook called after each retry attempt              |

<br/>

## 💡 Best Practices

### 1️⃣ Choose the Right Worker Count

- **CPU-bound tasks**: Set to `runtime.NumCPU()`
- **I/O-bound tasks**: Set higher (e.g., 2-4x CPU count)
- **External API calls**: Respect rate limits and consider timeouts

### 2️⃣ Use Context for Cancellation

Always pass a context with timeout or cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

results, err := pool.Process(ctx, tasks, processFn)
```

### 3️⃣ Handle Errors Appropriately

- Use retry policy for transient failures
- Implement proper error handling in your process function
- Use hooks to log or monitor errors

### 4️⃣ Rate Limiting for External Services

When calling external APIs, use rate limiting to avoid overwhelming them:

```go
pool.NewWorkerPool[T, R](
    pool.WithRateLimit(10.0, 5), // 10 req/sec, burst of 5
)
```

### 5️⃣ Thread-Safe Hooks

Ensure hook functions are thread-safe as they run concurrently:

```go
var mu sync.Mutex
var completed int

pool.WithOnTaskEnd(func(task int, result string, err error) {
    mu.Lock()
    completed++
    mu.Unlock()
})
```

<br/>

## ⚡ Performance

### Benchmark Results

Tested on Intel i7-11800H @ 2.30GHz (16 cores):

<div align="center">

| Metric                | Result                                  |
| --------------------- | --------------------------------------- |
| **Peak Throughput**   | ~1M tasks/sec (simple CPU tasks)        |
| **Worker Efficiency** | 400-500K tasks/sec/worker (2-4 workers) |
| **Memory per Task**   | ~65 bytes                               |
| **Parallel Speedup**  | 19x vs sequential (1000 tasks)          |

</div>

**Key Findings:**

- Buffer size 4-8x worker count provides ~30% throughput boost
- Optimal worker count: 8-16 for CPU-bound, 24-48 for I/O-bound tasks
- Minimal overhead: ~5% with hooks, ~1 allocation per task

**Run benchmarks:**

```bash
# Run all comprehensive benchmarks
go test -bench=BenchmarkComprehensive -benchmem ./benchmarks/

# Run specific benchmark categories
go test -bench=BenchmarkComprehensive_Throughput -benchmem ./benchmarks/
go test -bench=BenchmarkComprehensive_Modes -benchmem ./benchmarks/
go test -bench=BenchmarkComprehensive_Features -benchmem ./benchmarks/
go test -bench=BenchmarkComprehensive_Workload -benchmem ./benchmarks/
go test -bench=BenchmarkComprehensive_Memory -benchmem ./benchmarks/
go test -bench=BenchmarkComprehensive_Scenario -benchmem ./benchmarks/
```

**Note:** The comprehensive benchmarks are located in the [benchmarks](./benchmarks) directory as a separate package for better organization and modularity.

<br/>

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

<br/>

## 📄 License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

<br/>

---

<div align="center">

<img src="docs/public/logo.svg" alt="PoolMe Logo" width="80"/>

<br/><br/>

**Made with ❤️ by [utkarsh5026](https://github.com/utkarsh5026)**

<br/>

If you find this project helpful, please consider giving it a ⭐

<br/>

[Report Bug](https://github.com/utkarsh5026/poolme/issues) • [Request Feature](https://github.com/utkarsh5026/poolme/issues) • [Documentation](https://pkg.go.dev/github.com/utkarsh5026/poolme)

</div>
