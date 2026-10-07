package email

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	queueWorkers = 3
	queueSize    = 256
	sendTimeout  = 15 * time.Second
	sendAttempts = 3
	retryBackoff = time.Second
)

// ErrQueueFull means the queue is at capacity. Callers should tell the user to retry shortly.
var ErrQueueFull = errors.New("email queue is full")

// ErrQueueClosed means the queue is shutting down.
var ErrQueueClosed = errors.New("email queue is closed")

// Queue sends emails on a small in-process worker pool so HTTP handlers never wait on the provider.
// Jobs are not persisted: an email still queued when the process dies is lost, and the user asks for a new code.
type Queue struct {
	jobs   chan Message
	sender Sender
	wg     sync.WaitGroup

	mu     sync.RWMutex
	closed bool
}

func NewQueue(sender Sender) *Queue {
	return &Queue{jobs: make(chan Message, queueSize), sender: sender}
}

// Start launches the workers. They exit after Shutdown closes the queue and the backlog is drained.
func (q *Queue) Start() {
	for i := 0; i < queueWorkers; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for msg := range q.jobs {
				q.deliver(msg)
			}
		}()
	}
}

// Enqueue queues a message without blocking.
func (q *Queue) Enqueue(msg Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return ErrQueueClosed
	}
	select {
	case q.jobs <- msg:
		return nil
	default:
		return ErrQueueFull
	}
}

// Shutdown stops accepting messages and waits for the backlog to be sent, or for ctx to expire.
func (q *Queue) Shutdown(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	q.mu.Unlock()

	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliver sends one message, retrying transient failures. The recipient is never logged in full.
func (q *Queue) deliver(msg Message) {
	var err error
	for attempt := 1; attempt <= sendAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		err = q.sender.Send(ctx, msg)
		cancel()
		if err == nil {
			return
		}
		slog.Warn("send email failed", "subject", msg.Subject, "attempt", attempt, "err", err)
		if attempt < sendAttempts {
			time.Sleep(retryBackoff * time.Duration(attempt))
		}
	}
	slog.Error("email dropped after retries", "subject", msg.Subject, "err", err)
}
