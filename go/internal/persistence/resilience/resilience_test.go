package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestClassifyPersistenceErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FailureClass
	}{
		{name: "nil", err: nil, want: FailureClassIgnore},
		{name: "redis miss", err: redisv9.Nil, want: FailureClassIgnore},
		{name: "deadline", err: context.DeadlineExceeded, want: FailureClassRetryAsync},
		{name: "connection refused", err: errors.New("dial tcp 127.0.0.1:6379: connect: connection refused"), want: FailureClassRetryAsync},
		{name: "redis loading", err: errors.New("LOADING Redis is loading the dataset in memory"), want: FailureClassRetryAsync},
		{name: "wrong type", err: errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"), want: FailureClassBlock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			require.Equal(t, tt.want, got.Class)
		})
	}
}

func TestQueueRetriesAsyncUntilSuccess(t *testing.T) {
	queue := NewQueue(QueueConfig{
		Capacity:       1,
		MaxAttempts:    3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		AttemptTimeout: time.Second,
	})
	var attempts atomic.Int32
	done := make(chan struct{})

	ok := queue.Enqueue(context.Background(), Task{
		Operation: "cache_set",
		Resource:  "nof0:test",
		Do: func(ctx context.Context) error {
			require.True(t, IsRetryContext(ctx))
			attempt := attempts.Add(1)
			if attempt < 3 {
				return context.DeadlineExceeded
			}
			close(done)
			return nil
		},
	})

	require.True(t, ok)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry task did not complete")
	}
	require.Equal(t, int32(3), attempts.Load())
}

func TestQueueRejectsWhenSaturated(t *testing.T) {
	queue := NewQueue(QueueConfig{
		Capacity:       1,
		MaxAttempts:    1,
		AttemptTimeout: 50 * time.Millisecond,
	})
	block := make(chan struct{})

	accepted := queue.Enqueue(context.Background(), Task{
		Operation: "slow_task",
		Do: func(context.Context) error {
			<-block
			return nil
		},
	})
	require.True(t, accepted)

	rejected := queue.Enqueue(context.Background(), Task{
		Operation: "second_task",
		Do: func(context.Context) error {
			return nil
		},
	})
	close(block)
	require.False(t, rejected)
}
