package io

import (
	"math/rand"
)

// generateAPITasks creates tasks simulating API gateway workload
func generateAPITasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     15,
		latencyDecrease: 3,
		slowPercent:     95,
		latencyIncrease: 3,
	}
	return generateTasks(count, avgLatencyMs, "api", conf)
}

// generateDatabaseTasks creates tasks simulating database query workload
func generateDatabaseTasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     70,
		latencyDecrease: 2,
		slowPercent:     95,
		latencyIncrease: 5,
	}
	return generateTasks(count, avgLatencyMs, "database", conf)
}

// generateFileIOTasks creates tasks simulating file I/O workload
func generateFileIOTasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     50,
		latencyDecrease: 4,
		slowPercent:     90,
		latencyIncrease: 4,
	}
	return generateTasks(count, avgLatencyMs, "file", conf)
}

func generateTasks(count, avgLatencyMs int, taskType string, conf ioTaskGenConfig) []IOTask {
	tasks := make([]IOTask, count)
	for i := range count {
		latency := avgLatencyMs
		weight := "Medium"

		roll := rand.Intn(100)
		if roll < conf.fastPercent {
			latency = avgLatencyMs / conf.latencyDecrease
			weight = "Fast"
		} else if roll >= conf.slowPercent {
			latency = avgLatencyMs * conf.latencyIncrease
			weight = "Slow"
		}

		tasks[i] = IOTask{
			ID:          i,
			IOLatencyMs: latency,
			TaskType:    taskType,
			Weight:      weight,
		}
	}
	return tasks
}

// generateMixedTasks creates tasks with both I/O and CPU work
func generateMixedTasks(count int, avgLatencyMs int, cpuWork int) []IOTask {
	tasks := make([]IOTask, count)
	for i := range count {
		latency := avgLatencyMs
		cpu := cpuWork
		weight := "Medium"

		roll := rand.Intn(100)
		if roll < 60 {
			cpu = cpuWork / 4
			weight = "Fast"
		} else if roll >= 90 {
			cpu = cpuWork * 2
			latency = avgLatencyMs * 2
			weight = "Heavy"
		}

		tasks[i] = IOTask{
			ID:          i,
			IOLatencyMs: latency,
			CPUWork:     cpu,
			TaskType:    "mixed",
			Weight:      weight,
		}
	}
	return tasks
}
