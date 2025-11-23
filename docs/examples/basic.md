# Basic Examples

Learn the fundamentals of PoolMe with these basic examples.

## Simple Task Processing

The most basic usage: process a slice of integers.

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
    p := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(5),
    )

    // Tasks to process
    numbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

    // Process: square each number
    results, err := p.Process(
        context.Background(),
        numbers,
        func(ctx context.Context, n int) (int, error) {
            return n * n, nil
        },
    )

    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Results:", results)
    // Output: Results: [1 4 9 16 25 36 49 64 81 100]
}
```

## String Processing

Process strings and return transformed results.

```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    p := pool.NewWorkerPool[string, string](
        pool.WithWorkerCount(3),
    )

    words := []string{"hello", "world", "poolme", "golang"}

    results, _ := p.Process(
        context.Background(),
        words,
        func(ctx context.Context, word string) (string, error) {
            return strings.ToUpper(word), nil
        },
    )

    fmt.Println(results)
    // Output: [HELLO WORLD POOLME GOLANG]
}
```

## Working with Structs

Process custom types with complex logic.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/utkarsh5026/poolme/pool"
)

type User struct {
    ID   int
    Name string
}

type UserProfile struct {
    User      User
    Premium   bool
    CreatedAt time.Time
}

func main() {
    p := pool.NewWorkerPool[User, UserProfile](
        pool.WithWorkerCount(10),
    )

    users := []User{
        {ID: 1, Name: "Alice"},
        {ID: 2, Name: "Bob"},
        {ID: 3, Name: "Charlie"},
    }

    profiles, _ := p.Process(
        context.Background(),
        users,
        func(ctx context.Context, user User) (UserProfile, error) {
            // Simulate fetching user profile
            return UserProfile{
                User:      user,
                Premium:   user.ID%2 == 0, // Even IDs are premium
                CreatedAt: time.Now(),
            }, nil
        },
    )

    for _, profile := range profiles {
        fmt.Printf("%s - Premium: %v\n", profile.User.Name, profile.Premium)
    }
}
```

## Using Context for Timeout

Control execution time with context timeout.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    p := pool.NewWorkerPool[int, string](
        pool.WithWorkerCount(5),
    )

    // Create context with 2-second timeout
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    tasks := []int{1, 2, 3, 4, 5}

    results, err := p.Process(
        ctx,
        tasks,
        func(ctx context.Context, n int) (string, error) {
            // Check context cancellation
            select {
            case <-ctx.Done():
                return "", ctx.Err()
            case <-time.After(time.Duration(n) * 100 * time.Millisecond):
                return fmt.Sprintf("Task %d completed", n), nil
            }
        },
    )

    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    for _, result := range results {
        fmt.Println(result)
    }
}
```

## Error Handling

Handle errors from task processing.

```go
package main

import (
    "context"
    "fmt"
    "errors"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    p := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(3),
    )

    numbers := []int{1, 2, 0, 4, 5} // 0 will cause an error

    results, err := p.Process(
        context.Background(),
        numbers,
        func(ctx context.Context, n int) (int, error) {
            if n == 0 {
                return 0, errors.New("cannot divide by zero")
            }
            return 100 / n, nil
        },
    )

    if err != nil {
        fmt.Printf("Processing failed: %v\n", err)
        fmt.Printf("Partial results: %v\n", results)
        return
    }

    fmt.Println("Results:", results)
}
```

## Continue On Error

Process all tasks even if some fail.

```go
package main

import (
    "context"
    "fmt"
    "errors"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // Enable continueOnError
    p := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(3),
        pool.WithContinueOnError(true),
    )

    numbers := []int{1, 2, 0, 4, 5}

    results, err := p.Process(
        context.Background(),
        numbers,
        func(ctx context.Context, n int) (int, error) {
            if n == 0 {
                return 0, errors.New("zero encountered")
            }
            return n * 10, nil
        },
    )

    // err will be non-nil, but we get partial results
    if err != nil {
        fmt.Printf("Some tasks failed: %v\n", err)
    }

    fmt.Printf("Results: %v\n", results)
    // Output: Results: [10 20 0 40 50]
}
```

## Configuring Worker Count

Adjust workers based on your workload.

```go
package main

import (
    "context"
    "runtime"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // CPU-bound tasks: use number of CPUs
    cpuPool := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(runtime.NumCPU()),
    )

    // I/O-bound tasks: use more workers
    ioPool := pool.NewWorkerPool[string, string](
        pool.WithWorkerCount(runtime.NumCPU() * 4),
    )

    // Fixed number of workers
    fixedPool := pool.NewWorkerPool[int, string](
        pool.WithWorkerCount(10),
    )

    // Use the pools...
    _, _ = cpuPool.Process(context.Background(), []int{1, 2, 3}, nil)
    _, _ = ioPool.Process(context.Background(), []string{"a", "b"}, nil)
    _, _ = fixedPool.Process(context.Background(), []int{1}, nil)
}
```

## Task Buffer Size

Control memory usage with buffer size.

```go
package main

import (
    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // Small buffer (saves memory, may block producers)
    smallBuffer := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(5),
        pool.WithTaskBuffer(10),
    )

    // Large buffer (uses more memory, less blocking)
    largeBuffer := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(5),
        pool.WithTaskBuffer(1000),
    )

    // Default buffer (equals worker count)
    defaultBuffer := pool.NewWorkerPool[int, int](
        pool.WithWorkerCount(5),
        // taskBuffer defaults to 5
    )

    _, _, _ = smallBuffer, largeBuffer, defaultBuffer
}
```

## Chaining Pools

Chain multiple pools for multi-stage processing.

```go
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/utkarsh5026/poolme/pool"
)

func main() {
    // Stage 1: Convert to uppercase
    stage1 := pool.NewWorkerPool[string, string](
        pool.WithWorkerCount(3),
    )

    // Stage 2: Add prefix
    stage2 := pool.NewWorkerPool[string, string](
        pool.WithWorkerCount(3),
    )

    words := []string{"hello", "world", "golang"}

    // Process stage 1
    results1, _ := stage1.Process(
        context.Background(),
        words,
        func(ctx context.Context, word string) (string, error) {
            return strings.ToUpper(word), nil
        },
    )

    // Process stage 2 with results from stage 1
    results2, _ := stage2.Process(
        context.Background(),
        results1,
        func(ctx context.Context, word string) (string, error) {
            return "PREFIX_" + word, nil
        },
    )

    fmt.Println(results2)
    // Output: [PREFIX_HELLO PREFIX_WORLD PREFIX_GOLANG]
}
```

## Next Steps

Now that you understand the basics, explore:

- **[Batch Processing](/examples/batch)** - Process collections efficiently
- **[Streaming](/examples/streaming)** - Handle dynamic task streams
- **[Long-Running Pools](/examples/long-running)** - Use persistent worker pools
- **[API Reference](/api/worker-pool)** - Detailed method documentation
