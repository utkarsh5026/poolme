package scheduler

import (
	"context"
	"errors"

	"github.com/utkarsh5026/poolme/internal/types"
)

// schedulingStrategy defines the behavior for distributing tasks to workers.
// Any new algorithm (Work Stealing, Priority Queue, Ring Buffer) must implement this.
type SchedulingStrategy[T any, R any] interface {
	// Submit accepts a task into the scheduling system.
	// It handles the logic of where the task goes (Global Queue vs Local Queue).
	Submit(task *types.SubmittedTask[T, R]) error

	// SubmitBatch accepts multiple tasks at once for optimized batch submission.
	// Strategies can pre-distribute tasks to worker queues, avoiding per-task submission overhead.
	// Returns the number of tasks successfully submitted and an error if any occurred.
	SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error)

	// Shutdown gracefully stops the workers and waits for them to finish.
	Shutdown()

	// Worker executes tasks assigned to a specific worker, using the provided executor and pool.
	Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], resHandler types.ResultHandler[T, R]) error
}

func CreateSchedulingStrategy[T, R any](conf *ProcessorConfig[T, R], tasks []*types.SubmittedTask[T, R]) (SchedulingStrategy[T, R], error) {
	strategyType := conf.SchedulingStrategy

	if conf.UsePq {
		strategyType = SchedulingPriorityQueue
	}

	var s SchedulingStrategy[T, R]
	switch strategyType {
	case SchedulingWorkStealing:
		s = newWorkStealingStrategy(256, conf)

	case SchedulingPriorityQueue:
		if conf.LessFunc == nil {
			return nil, errors.New("priority queue enabled but no comparison function provided")
		}
		s = newPriorityQueueStrategy(conf, tasks)

	case SchedulingMPMC:
		s = newMPMCStrategy(conf, conf.MpmcBounded, conf.MpmcCapacity)

	case SchedulingBitmask:
		s = newBitmaskStrategy(conf)

	case SchedulingChannel:
		fallthrough
	default:
		s = newChannelStrategy(conf)
	}

	if conf.UseFusion {
		return newFusionStrategy(conf, s), nil
	}

	return s, nil
}
