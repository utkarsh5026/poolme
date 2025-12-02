package pool

import "testing"

// strategyConfig defines a test configuration for a scheduling strategy
type strategyConfig struct {
	name string
	opts []WorkerPoolOption
}

// getAllStrategies returns all scheduling strategies to test
// Each strategy is configured with appropriate options
func getAllStrategies(workerCount int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingWorkStealing),
			},
		},
		{
			name: "MPMC",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithMPMCQueue(WithBoundedQueue(1000)), // bounded with reasonable capacity
			},
		},
		{
			name: "PriorityQueue",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithPriorityQueue(func(a, b int) bool {
					return a < b
				}),
			},
		},
		{
			name: "SkipList",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSkipList(func(a, b int) bool {
					return a < b
				}),
			},
		},
		{
			name: "Bitmask",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingBitmask),
			},
		},
		{
			name: "Lmax",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithLmax(),
			},
		},
	}
}

// getAllStrategiesWithOpts returns all scheduling strategies with additional options
func getAllStrategiesWithOpts(workerCount int, additionalOpts ...WorkerPoolOption) []strategyConfig {
	baseStrategies := getAllStrategies(workerCount)
	for i := range baseStrategies {
		baseStrategies[i].opts = append(baseStrategies[i].opts, additionalOpts...)
	}
	return baseStrategies
}

func runStrategyTest(t *testing.T, testFunc func(t *testing.T, s strategyConfig), workerCount int, additionalOpts ...WorkerPoolOption) {
	strategies := getAllStrategiesWithOpts(workerCount, additionalOpts...)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			testFunc(t, strategy)
		})
	}
}
