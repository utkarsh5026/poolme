package cpu

import "slices"

// generateBalancedTasks creates tasks with uniform complexity
func generateBalancedTasks(count int, complexity int) []Task {
	tasks := make([]Task, count)
	for i := range count {
		tasks[i] = Task{
			ID:         i,
			Complexity: complexity,
			Weight:     "Medium",
		}
	}
	return tasks
}

// generateImbalancedTasks creates tasks with varying complexity
func generateImbalancedTasks(count int, baseComplexity int) []Task {
	tasks := make([]Task, count)
	heavyCount := count * 10 / 100
	mediumCount := count * 20 / 100

	for i := range count {
		task := Task{ID: i}

		if i < heavyCount {
			task.Complexity = baseComplexity * 10
			task.Weight = "Heavy"
		} else if i < heavyCount+mediumCount {
			task.Complexity = baseComplexity * 5
			task.Weight = "Medium"
		} else {
			task.Complexity = baseComplexity
			task.Weight = "Light"
		}

		tasks[i] = task
	}

	return tasks
}

// generatePriorityTasks creates imbalanced tasks in reversed order
func generatePriorityTasks(count int, baseComplexity int) []Task {
	tasks := generateImbalancedTasks(count, baseComplexity)
	slices.Reverse(tasks)
	return tasks
}
