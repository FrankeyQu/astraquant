package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cachekeys "nof0-api/internal/cache"
	persistresilience "nof0-api/internal/persistence/resilience"
)

func TestWritePositionsCacheQueuesRetryOnTransientCacheFailure(t *testing.T) {
	cache := &engineRecordingCache{setErr: context.DeadlineExceeded}
	retries := &engineRecordingRetryQueue{}
	svc := &Service{
		cache:      cache,
		ttl:        cachekeys.TTLSet{Medium: time.Minute},
		retryQueue: retries,
	}

	svc.writePositionsCache(context.Background(), "TRADER-A", map[string]positionCacheEntry{
		"BTC": {Symbol: "BTC", Side: "long", Quantity: 1},
	})

	tasks := retries.snapshot()
	require.Len(t, tasks, 1)
	require.Equal(t, engineCachePositionsOp, tasks[0].Operation)
	require.Equal(t, cachekeys.PositionsHashKey("TRADER-A"), tasks[0].Resource)
	require.Equal(t, persistresilience.FailureClassRetryAsync, tasks[0].Fields["failure_class"])
}

type engineRecordingRetryQueue struct {
	mu    sync.Mutex
	tasks []persistresilience.Task
}

func (q *engineRecordingRetryQueue) Enqueue(_ context.Context, task persistresilience.Task) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	return true
}

func (q *engineRecordingRetryQueue) snapshot() []persistresilience.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]persistresilience.Task, len(q.tasks))
	copy(out, q.tasks)
	return out
}

type engineRecordingCache struct {
	setErr error
}

func (c *engineRecordingCache) Del(...string) error {
	return nil
}

func (c *engineRecordingCache) DelCtx(context.Context, ...string) error {
	return nil
}

func (c *engineRecordingCache) Get(string, any) error {
	return errEngineCacheMiss
}

func (c *engineRecordingCache) GetCtx(context.Context, string, any) error {
	return errEngineCacheMiss
}

func (c *engineRecordingCache) IsNotFound(err error) bool {
	return errors.Is(err, errEngineCacheMiss)
}

func (c *engineRecordingCache) Set(string, any) error {
	return c.setErr
}

func (c *engineRecordingCache) SetCtx(context.Context, string, any) error {
	return c.setErr
}

func (c *engineRecordingCache) SetWithExpire(string, any, time.Duration) error {
	return c.setErr
}

func (c *engineRecordingCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error {
	return c.setErr
}

func (c *engineRecordingCache) Take(any, string, func(any) error) error {
	return errEngineCacheUnsupported
}

func (c *engineRecordingCache) TakeCtx(context.Context, any, string, func(any) error) error {
	return errEngineCacheUnsupported
}

func (c *engineRecordingCache) TakeWithExpire(any, string, func(any, time.Duration) error) error {
	return errEngineCacheUnsupported
}

func (c *engineRecordingCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return errEngineCacheUnsupported
}

var (
	errEngineCacheMiss        = errors.New("engine cache miss")
	errEngineCacheUnsupported = errors.New("engine cache unsupported")
)
