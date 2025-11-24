package pool

type priorityQueue[T any, R any] struct {
	queue      []*submittedTask[T, R]
	checkPrior func(a T) int
}

func newPriorityQueue[T any, R any](queue []*submittedTask[T, R], checkPrior func(a T) int) *priorityQueue[T, R] {
	return &priorityQueue[T, R]{
		queue:      queue,
		checkPrior: checkPrior,
	}
}

func (pq *priorityQueue[T, R]) Len() int {
	return len(pq.queue)
}

func (pq *priorityQueue[T, R]) Less(i, j int) bool {
	return pq.checkPrior(pq.queue[i].task) < pq.checkPrior(pq.queue[j].task)
}

func (pq *priorityQueue[T, R]) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
}

func (pq *priorityQueue[T, R]) Push(x any) {
	pq.queue = append(pq.queue, x.(*submittedTask[T, R]))
}

func (pq *priorityQueue[T, R]) Pop() any {
	old := pq.queue
	n := len(old)
	item := old[n-1]
	pq.queue = old[0 : n-1]
	return item
}
