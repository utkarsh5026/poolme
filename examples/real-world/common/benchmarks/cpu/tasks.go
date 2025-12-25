package cpu

import "time"

// Task represents a unit of work with configurable CPU complexity
type Task struct {
	ID         int
	Complexity int
	Weight     string
}

// TaskResult represents the result of processing a task
type TaskResult struct {
	ID          int
	ProcessTime time.Duration
}
