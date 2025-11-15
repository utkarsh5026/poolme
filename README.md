
# poolme

A small, simple, and generic worker pool for Go using generics (Go 1.18+). It provides a straightforward way to manage concurrent task processing.

<div align="center" style="margin-bottom: 1.5em;">
  <img src="./images/carbon%20(3).png" alt="Worker Pool Example" style="max-width: 90%; border-radius: 12px; box-shadow: 0 4px 24px rgba(0,0,0,0.1); margin-bottom: 0.5em;" />
  <br>
  <span style="display: block; font-size: 1.1em; color: #555; margin-top: 0.5em;"><b>Example: Simple Worker Pool in Action</b></span>
</div>


## Features

- **Generic:** Works with any task type (`T`) and result type (`R`).
- **Configurable:** Easily set the number of concurrent workers and the size of the task buffer.
- **Context-Aware:** Supports `context.Context` for cancellation and timeouts.
- **Robust:** Includes error propagation and panic recovery in workers.
- **Flexible:** Process tasks from a slice (`Process`), a map (`ProcessMap`), or a channel (`ProcessStream`).

## Installation

```sh
go get github.com/utkarsh5026/poolme
```

## Basic Usage

Here's how to process a slice of tasks. The results are returned in a slice, preserving the original order.

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

	// Create a new pool with 4 workers
	p := pool.NewWorkerPool[int, string](pool.WithWorkerCount(4))

	// Define the processing function
	processFn := func(ctx context.Context, task int) (string, error) {
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		return fmt.Sprintf("Processed task %d", task), nil
	}

	// Process the tasks
	results, err := p.Process(ctx, tasks, processFn)
	if err != nil {
		panic(err)
	}

	fmt.Println(results)
	// Output:
	// [Processed task 1 Processed task 2 Processed task 3 Processed task 4 Processed task 5 Processed task 6 Processed task 7 Processed task 8]
}
```

## License

This project is licensed under the Apache License 2.0.
