package email

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type countingSender struct{ n atomic.Int32 }

func (s *countingSender) Send(context.Context, Message) error { s.n.Add(1); return nil }

func TestQueueDrainsOnShutdown(t *testing.T) {
	s := &countingSender{}
	q := NewQueue(s)
	q.Start()
	for i := 0; i < 10; i++ {
		if err := q.Enqueue(Message{To: "a@b.co"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if s.n.Load() != 10 {
		t.Errorf("sent %d, want 10", s.n.Load())
	}
	if err := q.Enqueue(Message{}); !errors.Is(err, ErrQueueClosed) {
		t.Errorf("got %v, want ErrQueueClosed", err)
	}
}

func TestQueueFull(t *testing.T) {
	q := NewQueue(&countingSender{}) // workers not started, so nothing drains
	for i := 0; i < queueSize; i++ {
		if err := q.Enqueue(Message{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Enqueue(Message{}); !errors.Is(err, ErrQueueFull) {
		t.Errorf("got %v, want ErrQueueFull", err)
	}
}
