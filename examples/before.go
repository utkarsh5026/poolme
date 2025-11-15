package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// User represents a user fetched from an API
type User struct {
	ID       int
	Name     string
	Email    string
	Response string
}

// BEFORE: Manual goroutine management
// This is what developers typically write without a worker pool library.
// Notice the complexity: manual channel management, error handling, worker coordination,
// result ordering, panic recovery, context cancellation, and proper cleanup.

func fetchUserDataManually(ctx context.Context, userIDs []int) ([]User, error) {
	if len(userIDs) == 0 {
		return []User{}, nil
	}

	// Set up channels and synchronization primitives
	numWorkers := 4
	taskChan := make(chan struct {
		id    int
		index int
	}, numWorkers)
	resultChan := make(chan struct {
		user  User
		index int
		err   error
	}, len(userIDs))

	// Create context for cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Track errors
	var errMutex sync.Mutex
	var firstError error

	// WaitGroup for workers
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				// Panic recovery - each worker needs this
				if r := recover(); r != nil {
					errMutex.Lock()
					if firstError == nil {
						firstError = fmt.Errorf("worker %d panic: %v", workerID, r)
					}
					errMutex.Unlock()
					cancel() // Cancel all other workers
				}
			}()

			for {
				select {
				case task, ok := <-taskChan:
					if !ok {
						return
					}

					// Simulate API call
					user, err := fetchUser(ctx, task.id)

					// Check for errors
					if err != nil {
						errMutex.Lock()
						if firstError == nil {
							firstError = err
						}
						errMutex.Unlock()
						cancel() // Cancel on error
						return
					}

					// Send result
					select {
					case resultChan <- struct {
						user  User
						index int
						err   error
					}{user: user, index: task.index, err: nil}:
					case <-ctx.Done():
						return
					}

				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Send tasks to workers
	go func() {
		defer close(taskChan)
		for idx, id := range userIDs {
			select {
			case taskChan <- struct {
				id    int
				index int
			}{id: id, index: idx}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Collect results in order
	results := make([]User, len(userIDs))
	var collectionWg sync.WaitGroup
	collectionWg.Add(1)

	go func() {
		defer collectionWg.Done()
		received := 0
		for result := range resultChan {
			if result.err != nil {
				continue
			}
			if result.index >= 0 && result.index < len(results) {
				results[result.index] = result.user
			}
			received++
			if received == len(userIDs) {
				return
			}
		}
	}()

	// Wait for all workers to complete
	wg.Wait()
	close(resultChan)
	collectionWg.Wait()

	if firstError != nil {
		return results, firstError
	}

	return results, nil
}

func fetchUser(ctx context.Context, userID int) (User, error) {
	// Simulate API call with timeout
	time.Sleep(100 * time.Millisecond)

	// Simulate occasional API call
	url := fmt.Sprintf("https://jsonplaceholder.typicode.com/users/%d", userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return User{}, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// For demo purposes, return simulated data on error
		return User{
			ID:       userID,
			Name:     fmt.Sprintf("User %d", userID),
			Email:    fmt.Sprintf("user%d@example.com", userID),
			Response: "simulated",
		}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return User{
		ID:       userID,
		Name:     fmt.Sprintf("User %d", userID),
		Email:    fmt.Sprintf("user%d@example.com", userID),
		Response: string(body),
	}, nil
}

func main() {
	ctx := context.Background()
	userIDs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	start := time.Now()
	users, err := fetchUserDataManually(ctx, userIDs)
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("✓ Successfully processed %d users in %v\n", len(users), duration)
	for _, user := range users {
		fmt.Printf("  - %s (%s)\n", user.Name, user.Email)
	}
}
