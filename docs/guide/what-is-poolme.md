# What is PoolMe?

PoolMe is a production-ready, type-safe worker pool library for Go that makes concurrent task processing simple and efficient.

## Overview

PoolMe provides a generic, high-performance worker pool implementation that allows you to process tasks concurrently with full type safety, context support, and robust error handling.

## Key Features

### 🎯 Type Safety with Generics

Built with Go 1.18+ generics for compile-time type safety:

```go
// Define your types explicitly
pool := NewWorkerPool[InputType, OutputType](...)

// No interface{} conversions needed
results, err := pool.Process(ctx, tasks, processFunc)
// results is []OutputType, fully typed!
```

### 🔄 Multiple Processing Modes

Choose the right mode for your use case:

1. **Batch Processing** - Process a known set of tasks
2. **Map Processing** - Process keyed collections
3. **Streaming** - Handle dynamic task streams
4. **Long-Running Pool** - Persistent workers with async submission

### ⚡ High Performance

- Configurable worker count for optimal concurrency
- Efficient task distribution with minimal overhead
- Controlled resource usage with buffer management
- Processes thousands of tasks per second

### 🛡️ Robust Error Handling

- Retry logic with exponential backoff
- Continue-on-error option for fault tolerance
- Detailed error reporting and callbacks
- Graceful degradation

### 🎛️ Highly Configurable

Fine-tune behavior with options:
- Worker count
- Task buffer size
- Retry attempts and delays
- Rate limiting
- Lifecycle hooks
- Error handling strategy

## Use Cases

### Web Scraping
Process multiple URLs concurrently with rate limiting:

```go
pool := NewWorkerPool[string, Page](
    WithWorkerCount(10),
    WithRateLimiter(rate.NewLimiter(5, 1)), // 5 req/sec
    WithMaxAttempts(3),
)

pages, _ := pool.Process(ctx, urls, scrapeURL)
```

### Data Processing
Transform large datasets in parallel:

```go
pool := NewWorkerPool[Record, ProcessedRecord](
    WithWorkerCount(runtime.NumCPU()),
)

results, _ := pool.Process(ctx, records, processRecord)
```

### API Integration
Make concurrent API calls with backoff:

```go
pool := NewWorkerPool[Request, Response](
    WithWorkerCount(20),
    WithMaxAttempts(3),
    WithInitialDelay(100 * time.Millisecond),
)

responses, _ := pool.Process(ctx, requests, makeAPICall)
```

### Image Processing
Resize or transform images concurrently:

```go
pool := NewWorkerPool[ImagePath, ProcessedImage](
    WithWorkerCount(8),
)

images, _ := pool.Process(ctx, imagePaths, processImage)
```

## Why Choose PoolMe?

### vs. Standard Go Concurrency

**Standard Approach:**
```go
// Manual goroutine management
var wg sync.WaitGroup
results := make([]Result, len(tasks))
errors := make(chan error, len(tasks))

for i, task := range tasks {
    wg.Add(1)
    go func(i int, task Task) {
        defer wg.Done()
        // Need to handle errors, retries, rate limiting manually...
        result, err := process(task)
        if err != nil {
            errors <- err
            return
        }
        results[i] = result
    }(i, task)
}
wg.Wait()
```

**With PoolMe:**
```go
pool := NewWorkerPool[Task, Result](WithWorkerCount(10))
results, err := pool.Process(ctx, tasks, process)
```

### vs. Other Worker Pool Libraries

| Feature | PoolMe | Others |
|---------|--------|--------|
| **Type Safety** | ✅ Full generics | ❌ interface{} |
| **Context Support** | ✅ Built-in | ⚠️ Limited |
| **Retry Logic** | ✅ Configurable | ❌ Manual |
| **Rate Limiting** | ✅ Integrated | ❌ External |
| **Streaming** | ✅ Native | ⚠️ Basic |
| **Zero Dependencies** | ✅ (except rate) | ❌ Many deps |

## Architecture

```
                    ┌─────────────────┐
                    │   WorkerPool    │
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │  Task Channel   │
                    └─────┬─┬─┬─┬─┬───┘
                          │ │ │ │ │
              ┌───────────┘ │ │ │ └───────────┐
              │             │ │ │             │
         ┌────▼────┐   ┌───▼─▼─▼───┐   ┌────▼────┐
         │ Worker  │   │  Workers   │   │ Worker  │
         │   #1    │   │  #2...#N-1 │   │   #N    │
         └────┬────┘   └─────┬──────┘   └────┬────┘
              │              │              │
              └──────────┬───┴──────────────┘
                         │
                    ┌────▼────┐
                    │ Results │
                    └─────────┘
```

## Core Concepts

### Workers
Independent goroutines that process tasks concurrently.

### Task Channel
Buffered channel that queues tasks for processing.

### Context
Used for cancellation, timeouts, and resource cleanup.

### ProcessFunc
User-defined function that processes each task.

### Future
Promise-like object for async result retrieval.

## Getting Started

Ready to use PoolMe? Head to:
- [Getting Started Guide](/guide/getting-started) - Installation and first pool
- [Examples](/examples/basic) - Real-world usage patterns
- [API Reference](/api/worker-pool) - Complete API documentation

## Philosophy

PoolMe is designed with these principles:

1. **Simplicity** - Easy to use with sensible defaults
2. **Type Safety** - Leverage Go's type system
3. **Performance** - Efficient resource usage
4. **Reliability** - Production-ready with proper error handling
5. **Flexibility** - Configurable for different use cases

---

**Next:** Learn how to [get started](/guide/getting-started) with PoolMe.
