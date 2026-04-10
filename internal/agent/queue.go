package agent

import "sync"

type queuedChannel[T any] struct {
	out       chan T
	mu        sync.Mutex
	cond      *sync.Cond
	queue     []T
	closed    bool
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newQueuedChannel[T any](buffer int) *queuedChannel[T] {
	q := &queuedChannel[T]{
		out:     make(chan T, buffer),
		closeCh: make(chan struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	go q.pump()
	return q
}

func (q *queuedChannel[T]) Channel() <-chan T {
	if q == nil {
		return nil
	}
	return q.out
}

func (q *queuedChannel[T]) Send(value T) bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.queue = append(q.queue, value)
	q.cond.Signal()
	return true
}

func (q *queuedChannel[T]) Close() {
	if q == nil {
		return
	}
	q.closeOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		q.cond.Broadcast()
		q.mu.Unlock()
		<-q.closeCh
	})
}

func (q *queuedChannel[T]) pump() {
	defer close(q.closeCh)
	for {
		q.mu.Lock()
		for len(q.queue) == 0 && !q.closed {
			q.cond.Wait()
		}
		if len(q.queue) == 0 && q.closed {
			q.mu.Unlock()
			close(q.out)
			return
		}
		value := q.queue[0]
		var zero T
		q.queue[0] = zero
		q.queue = q.queue[1:]
		q.mu.Unlock()

		q.out <- value
	}
}
