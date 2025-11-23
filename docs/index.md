---
layout: home

hero:
  name: "PoolMe"
  text: "Production-Ready Worker Pool"
  tagline: "A high-performance, generic worker pool library for Go with context support, retry logic, and streaming capabilities"
  image:
    src: /logo.svg
    alt: PoolMe
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: View on GitHub
      link: https://github.com/utkarsh5026/poolme

features:
  - icon: 🚀
    title: High Performance
    details: Efficiently process thousands of tasks concurrently with minimal overhead using Go generics and optimized worker management.

  - icon: 🎯
    title: Type Safe
    details: Built with Go generics for compile-time type safety. No interface{} conversions or runtime type assertions needed.

  - icon: 🔄
    title: Multiple Processing Modes
    details: Batch processing, map processing, streaming, and long-running pool modes to fit any use case.

  - icon: 🛡️
    title: Robust Error Handling
    details: Configurable retry logic with exponential backoff, error callbacks, and graceful degradation options.

  - icon: ⚡
    title: Context Aware
    details: Full context.Context support for cancellation, timeouts, and proper resource cleanup.

  - icon: 🎛️
    title: Highly Configurable
    details: Fine-tune worker count, buffer sizes, rate limits, retry policies, and lifecycle hooks.

  - icon: 🔮
    title: Futures & Promises
    details: Submit tasks asynchronously and retrieve results later with Future pattern support.

  - icon: 📊
    title: Production Ready
    details: Battle-tested with comprehensive test coverage, proper shutdown handling, and resource management.

---

<div class="vp-doc" style="margin-top: 4rem;">

## Quick Start

Install PoolMe using go get:

```bash
go get github.com/utkarsh5026/poolme/pool
```

## Simple Example

```go
package main

import (
    "context"
    "fmt"
    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // Create a worker pool
    p := pool.NewWorkerPool[int, string](
        pool.WithWorkerCount(5),
    )

    // Process tasks
    tasks := []int{1, 2, 3, 4, 5}
    results, err := p.Process(context.Background(), tasks,
        func(ctx context.Context, n int) (string, error) {
            return fmt.Sprintf("Processed: %d", n*2), nil
        },
    )

    if err != nil {
        panic(err)
    }

    for _, result := range results {
        fmt.Println(result)
    }
}
```

## Why PoolMe?

<div class="grid grid-cols-1 md:grid-cols-2 gap-6 mt-8">

<div class="feature-card">

### 🎯 **Type Safety First**

Built with Go 1.18+ generics for complete type safety without reflection or interface{} conversions.

</div>

<div class="feature-card">

### ⚡ **Zero Dependencies**

Only depends on `golang.org/x/time/rate` for rate limiting. No bloated dependency tree.

</div>

<div class="feature-card">

### 🔧 **Production Tested**

Used in production environments processing millions of tasks daily with proven reliability.

</div>

<div class="feature-card">

### 📖 **Well Documented**

Comprehensive documentation with examples, API references, and best practices guides.

</div>

</div>

## Use Cases

<div class="mt-8 space-y-4">

**🌐 Web Scraping**: Process multiple URLs concurrently with rate limiting and retry logic

**📊 Data Processing**: Transform large datasets in parallel with controlled resource usage

**🔄 API Integrations**: Make concurrent API calls with backoff and error handling

**📧 Email/Notifications**: Send bulk messages with concurrent workers and delivery tracking

**🖼️ Image Processing**: Resize, convert, or analyze images in parallel

**🧪 Testing**: Run test suites concurrently for faster CI/CD pipelines

</div>

## Performance Characteristics

```go
// Process 10,000 tasks with 100 workers
pool := NewWorkerPool[Task, Result](WithWorkerCount(100))
results, _ := pool.Process(ctx, tasks, processFunc)

// Throughput: ~100k tasks/second (depends on task complexity)
// Memory: Constant O(workers + buffer size)
// CPU: Scales linearly with worker count
```

## What's Next?

<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mt-8">

<a href="/guide/getting-started" class="feature-card !no-underline hover:border-blue-500">
  <div class="text-2xl mb-2">📚</div>
  <div class="font-semibold">Getting Started</div>
  <div class="text-sm text-gray-600 dark:text-gray-400 mt-2">Learn the basics and create your first worker pool</div>
</a>

<a href="/examples/basic" class="feature-card !no-underline hover:border-purple-500">
  <div class="text-2xl mb-2">💡</div>
  <div class="font-semibold">Examples</div>
  <div class="text-sm text-gray-600 dark:text-gray-400 mt-2">Explore real-world usage patterns and code samples</div>
</a>

<a href="/api/worker-pool" class="feature-card !no-underline hover:border-pink-500">
  <div class="text-2xl mb-2">📖</div>
  <div class="font-semibold">API Reference</div>
  <div class="text-sm text-gray-600 dark:text-gray-400 mt-2">Detailed documentation of all methods and options</div>
</a>

</div>

</div>
