package io

import (
	"sync/atomic"
	"time"
)

// IOTask represents a task with I/O characteristics
type IOTask struct {
	ID          int
	IOLatencyMs int
	CPUWork     int
	TaskType    string
	Weight      string
}

// IOTaskResult represents the result of processing an I/O task
type IOTaskResult struct {
	ID          int
	ProcessTime time.Duration
	IOTime      time.Duration
	CPUTime     time.Duration
	TaskType    string
}

type ioTaskGenConfig struct {
	fastPercent, latencyDecrease int
	slowPercent, latencyIncrease int
}

var (
	// Global counters for simulated operations
	totalAPICalls  atomic.Int64
	totalDBQueries atomic.Int64
	totalFileOps   atomic.Int64
)
