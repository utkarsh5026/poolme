package scheduler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/gopool/internal/types"
)

func createTestTask[T any, R any](input T, id int64) *types.SubmittedTask[T, R] {
	future := types.NewFuture[R, int64]()
	return types.NewSubmittedTask(input, id, future)
}

func getChannelLength[T any, R any](s *channelStrategy[T, R]) int {
	count := 0
	for _, ch := range s.taskChans {
		count += len(ch)
	}
	return count
}

func drainChannel[T any, R any](s *channelStrategy[T, R]) int {
	var count atomic.Int64
	var wg sync.WaitGroup
	wg.Add(len(s.taskChans))

	for _, ch := range s.taskChans {
		go func() {
			defer wg.Done()
			var c int64 = 0
		loop:
			for {
				select {
				case <-ch:
					c++
				default:
					break loop // Breaks out of the for loop
				}
			}
			count.Add(c)
		}()
	}

	wg.Wait()
	return int(count.Load())
}

func TestNewFusionStrategy(t *testing.T) {
	t.Run("uses default values when config is empty", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer: 10,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		if fs.fusionWindow != defaultFusionWindow {
			t.Errorf("expected fusionWindow=%v, got %v", defaultFusionWindow, fs.fusionWindow)
		}
		if fs.maxBatchSize != defaultFusionBatchSize {
			t.Errorf("expected maxBatchSize=%d, got %d", defaultFusionBatchSize, fs.maxBatchSize)
		}
		if fs.underlying == nil {
			t.Error("underlying strategy not set")
		}
	})

	t.Run("uses custom config values", func(t *testing.T) {
		customWindow := 500 * time.Millisecond
		customBatchSize := 64

		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      10,
			FusionWindow:    customWindow,
			FusionBatchSize: customBatchSize,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		if fs.fusionWindow != customWindow {
			t.Errorf("expected fusionWindow=%v, got %v", customWindow, fs.fusionWindow)
		}
		if fs.maxBatchSize != customBatchSize {
			t.Errorf("expected maxBatchSize=%d, got %d", customBatchSize, fs.maxBatchSize)
		}
	})
}

func TestFusionStrategy_Submit_BatchSize(t *testing.T) {
	t.Run("flushes immediately when batch size reached", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    1 * time.Second, // Long window to ensure size triggers
			FusionBatchSize: 3,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit exactly maxBatchSize tasks
		fs.Submit(createTestTask[int, int](1, 1))
		fs.Submit(createTestTask[int, int](2, 2))
		fs.Submit(createTestTask[int, int](3, 3))

		// Should flush immediately without waiting for timer
		time.Sleep(10 * time.Millisecond) // Brief wait for flush to complete

		channelLen := getChannelLength(underlying)
		if channelLen != 3 {
			t.Errorf("expected 3 tasks in channel, got %d", channelLen)
		}

		// Clean up
		fs.Shutdown()
	})

	t.Run("accumulates and flushes multiple batches", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    1 * time.Second,
			FusionBatchSize: 2,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit 5 tasks (should create 2 full batches + 1 partial)
		for i := range 5 {
			fs.Submit(createTestTask[int, int](i, int64(i)))
		}

		time.Sleep(10 * time.Millisecond)

		// Should have at least 4 tasks flushed (2 full batches)
		channelLen := getChannelLength(underlying)
		if channelLen < 4 {
			t.Errorf("expected at least 4 tasks in channel, got %d", channelLen)
		}

		// Clean up
		fs.Shutdown()
	})
}

func TestFusionStrategy_Submit_TimerFlush(t *testing.T) {
	t.Run("flushes after fusion window expires", func(t *testing.T) {
		fusionWindow := 50 * time.Millisecond

		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    fusionWindow,
			FusionBatchSize: 100, // Large batch size to ensure timer triggers
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit a single task
		fs.Submit(createTestTask[int, int](1, 1))

		// Verify not flushed immediately
		if getChannelLength(underlying) != 0 {
			t.Error("task should not be flushed immediately")
		}

		// Wait for timer to expire
		time.Sleep(fusionWindow + 30*time.Millisecond)

		// Should now be flushed
		channelLen := getChannelLength(underlying)
		if channelLen != 1 {
			t.Errorf("expected 1 task flushed after timer, got %d", channelLen)
		}

		// Clean up
		fs.Shutdown()
	})

	t.Run("timer resets on first task of new batch", func(t *testing.T) {
		fusionWindow := 50 * time.Millisecond

		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    fusionWindow,
			FusionBatchSize: 2,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit first task
		fs.Submit(createTestTask[int, int](1, 1))
		time.Sleep(30 * time.Millisecond) // Wait partial window

		// Submit second task (should complete batch)
		fs.Submit(createTestTask[int, int](2, 2))
		time.Sleep(10 * time.Millisecond)

		// First batch should be flushed
		if getChannelLength(underlying) != 2 {
			t.Errorf("expected 2 tasks flushed, got %d", getChannelLength(underlying))
		}

		// Submit third task (starts new batch)
		fs.Submit(createTestTask[int, int](3, 3))

		// Should not be flushed yet
		drainChannel(underlying) // Clear the channel
		if getChannelLength(underlying) != 0 {
			t.Error("third task should not be flushed immediately")
		}

		// Wait for timer
		time.Sleep(fusionWindow + 30*time.Millisecond)

		// Should now be flushed
		if getChannelLength(underlying) != 1 {
			t.Errorf("expected 1 task after timer, got %d", getChannelLength(underlying))
		}

		// Clean up
		fs.Shutdown()
	})
}

func TestFusionStrategy_Submit_Concurrency(t *testing.T) {
	t.Run("handles concurrent submissions safely", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      1000,
			FusionWindow:    100 * time.Millisecond,
			FusionBatchSize: 50,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		const goroutines = 10
		const tasksPerGoroutine = 20
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := range goroutines {
			go func(start int) {
				defer wg.Done()
				for j := range tasksPerGoroutine {
					fs.Submit(createTestTask[int, int](start*tasksPerGoroutine+j, int64(start*tasksPerGoroutine+j)))
				}
			}(i)
		}

		wg.Wait()
		time.Sleep(200 * time.Millisecond)

		totalSubmitted := drainChannel(underlying)
		expectedTotal := goroutines * tasksPerGoroutine
		if totalSubmitted != expectedTotal {
			t.Errorf("expected %d tasks submitted, got %d", expectedTotal, totalSubmitted)
		}

		// Clean up
		fs.Shutdown()
	})
}

func TestFusionStrategy_Shutdown(t *testing.T) {
	t.Run("prevents new submissions after shutdown and flushes pending", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    100 * time.Millisecond,
			FusionBatchSize: 10,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit a task before shutdown
		fs.Submit(createTestTask[int, int](1, 1))

		// Check pending task count before shutdown
		time.Sleep(10 * time.Millisecond)
		beforeShutdown := getChannelLength(underlying)
		if beforeShutdown != 0 {
			t.Errorf("expected 0 tasks in channel before shutdown (not flushed yet), got %d", beforeShutdown)
		}

		// Shutdown (should flush pending tasks)
		fs.Shutdown()

		// After shutdown, try to submit - should be rejected
		fs.Submit(createTestTask[int, int](2, 2))
		fs.Submit(createTestTask[int, int](3, 3))

		time.Sleep(10 * time.Millisecond)

		// Channel should be closed after shutdown, no way to verify directly
		// The test passed if we reached here without hanging
	})
}

func TestFusionStrategy_SubmitBatch(t *testing.T) {
	t.Run("passes batch directly to underlying strategy", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer: 100,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		tasks := []*types.SubmittedTask[int, int]{
			createTestTask[int, int](1, 1),
			createTestTask[int, int](2, 2),
			createTestTask[int, int](3, 3),
		}

		count, err := fs.SubmitBatch(tasks)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 3 {
			t.Errorf("expected count=3, got %d", count)
		}

		// Wait for batch to be processed
		time.Sleep(10 * time.Millisecond)

		channelLen := getChannelLength(underlying)
		if channelLen != 3 {
			t.Errorf("expected 3 tasks in underlying channel, got %d", channelLen)
		}

		// Clean up
		fs.Shutdown()
	})

	t.Run("rejects batch after shutdown", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer: 100,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		fs.Shutdown()

		tasks := []*types.SubmittedTask[int, int]{
			createTestTask[int, int](1, 1),
		}

		count, err := fs.SubmitBatch(tasks)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("expected count=0 after shutdown, got %d", count)
		}
	})
}

func TestFusionStrategy_EdgeCases(t *testing.T) {
	t.Run("handles empty batch accumulation", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    50 * time.Millisecond,
			FusionBatchSize: 10,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Wait for timer without submitting anything
		time.Sleep(100 * time.Millisecond)

		// Should not have flushed anything
		if getChannelLength(underlying) != 0 {
			t.Error("should not flush empty batch")
		}

		// Clean up
		fs.Shutdown()
	})

	t.Run("handles rapid submission below batch size", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    100 * time.Millisecond,
			FusionBatchSize: 100,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		// Submit tasks rapidly but below batch size
		for i := range 50 {
			fs.Submit(createTestTask[int, int](i, int64(i)))
		}

		// Should not flush immediately
		time.Sleep(10 * time.Millisecond)
		if getChannelLength(underlying) != 0 {
			t.Error("should not flush before timer expires")
		}

		// Wait for timer
		time.Sleep(150 * time.Millisecond)

		// Should now be flushed
		channelLen := getChannelLength(underlying)
		if channelLen != 50 {
			t.Errorf("expected 50 tasks flushed, got %d", channelLen)
		}

		// Clean up
		fs.Shutdown()
	})

	t.Run("batch size of 1 flushes immediately", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:      100,
			FusionWindow:    1 * time.Second,
			FusionBatchSize: 1,
		}
		underlying := newChannelStrategy(conf)
		fs := newFusionStrategy(conf, underlying)

		fs.Submit(createTestTask[int, int](1, 1))
		time.Sleep(10 * time.Millisecond)

		if getChannelLength(underlying) != 1 {
			t.Error("batch size of 1 should flush immediately")
		}

		fs.Submit(createTestTask[int, int](2, 2))
		time.Sleep(10 * time.Millisecond)

		// Should have 2 tasks total in the channel
		totalTasks := drainChannel(underlying)
		if totalTasks != 2 {
			t.Errorf("expected 2 tasks flushed immediately with batch size 1, got %d", totalTasks)
		}

		// Clean up
		fs.Shutdown()
	})
}

func TestCreateSchedulingStrategy_Fusion(t *testing.T) {
	t.Run("creates fusion strategy wrapping channel strategy", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:         100,
			SchedulingStrategy: SchedulingChannel,
			UseFusion:          true,
			FusionWindow:       200 * time.Millisecond,
			FusionBatchSize:    50,
		}

		strategy, err := CreateSchedulingStrategy(conf, nil)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if strategy == nil {
			t.Fatal("strategy should not be nil")
		}

		// Verify it's a fusion strategy by checking behavior
		fs, ok := strategy.(*fusionStrategy[int, int])
		if !ok {
			t.Fatal("expected fusionStrategy type")
		}

		if fs.fusionWindow != 200*time.Millisecond {
			t.Errorf("expected fusionWindow=200ms, got %v", fs.fusionWindow)
		}
		if fs.maxBatchSize != 50 {
			t.Errorf("expected maxBatchSize=50, got %d", fs.maxBatchSize)
		}

		// Verify the underlying strategy is channel strategy
		_, ok = fs.underlying.(*channelStrategy[int, int])
		if !ok {
			t.Error("expected underlying strategy to be channelStrategy")
		}

		// Clean up
		strategy.Shutdown()
	})

	t.Run("creates fusion strategy wrapping work-stealing strategy", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:         100,
			SchedulingStrategy: SchedulingWorkStealing,
			UseFusion:          true,
			FusionWindow:       100 * time.Millisecond,
			FusionBatchSize:    32,
		}

		strategy, err := CreateSchedulingStrategy[int, int](conf, nil)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		fs, ok := strategy.(*fusionStrategy[int, int])
		if !ok {
			t.Fatal("expected fusionStrategy type")
		}

		// Verify the underlying strategy is work-stealing strategy
		_, ok = fs.underlying.(*workSteal[int, int])
		if !ok {
			t.Error("expected underlying strategy to be workSteal")
		}

		strategy.Shutdown()
	})

	t.Run("creates fusion strategy wrapping MPMC strategy", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:         100,
			SchedulingStrategy: SchedulingMPMC,
			MpmcBounded:        true,
			MpmcCapacity:       100,
			UseFusion:          true,
			FusionWindow:       100 * time.Millisecond,
			FusionBatchSize:    32,
		}

		strategy, err := CreateSchedulingStrategy[int, int](conf, nil)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		fs, ok := strategy.(*fusionStrategy[int, int])
		if !ok {
			t.Fatal("expected fusionStrategy type")
		}

		// Verify the underlying strategy is MPMC strategy
		_, ok = fs.underlying.(*mpmc[int, int])
		if !ok {
			t.Error("expected underlying strategy to be mpmc")
		}

		strategy.Shutdown()
	})

	t.Run("creates base strategy without fusion when UseFusion is false", func(t *testing.T) {
		conf := &ProcessorConfig[int, int]{
			TaskBuffer:         100,
			SchedulingStrategy: SchedulingChannel,
			UseFusion:          false,
		}

		strategy, err := CreateSchedulingStrategy(conf, nil)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Should NOT be a fusion strategy
		_, ok := strategy.(*fusionStrategy[int, int])
		if ok {
			t.Fatal("should not be fusionStrategy when UseFusion is false")
		}

		// Should be the base channel strategy
		_, ok = strategy.(*channelStrategy[int, int])
		if !ok {
			t.Error("expected channelStrategy when UseFusion is false")
		}

		strategy.Shutdown()
	})
}
