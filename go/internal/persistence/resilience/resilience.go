package resilience

import (
	"context"
	"errors"
	"math"
	"net"
	"strings"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultQueueCapacity  = 32
	defaultMaxAttempts    = 3
	defaultInitialBackoff = 250 * time.Millisecond
	defaultMaxBackoff     = 5 * time.Second
	defaultAttemptTimeout = 2 * time.Second
)

// FailureClass describes how a persistence failure should be handled.
type FailureClass string

const (
	FailureClassBlock      FailureClass = "block"
	FailureClassRetryAsync FailureClass = "retry_async"
	FailureClassIgnore     FailureClass = "ignore"
)

// Classification is the outcome of error classification.
type Classification struct {
	Class  FailureClass
	Reason string
}

// Task describes a retryable persistence operation.
type Task struct {
	Operation      string
	Resource       string
	Fields         map[string]any
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	AttemptTimeout time.Duration
	Retryable      func(error) bool
	Do             func(context.Context) error
}

// QueueConfig controls async retry behavior.
type QueueConfig struct {
	Capacity       int
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	AttemptTimeout time.Duration
}

// Enqueuer accepts retry tasks.
type Enqueuer interface {
	Enqueue(context.Context, Task) bool
}

// Queue is a bounded async retry executor.
type Queue struct {
	cfg QueueConfig
	sem chan struct{}
}

// NewQueue constructs a retry queue with sane defaults.
func NewQueue(cfg QueueConfig) *Queue {
	if cfg.Capacity <= 0 {
		cfg.Capacity = defaultQueueCapacity
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaultInitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}
	if cfg.AttemptTimeout <= 0 {
		cfg.AttemptTimeout = defaultAttemptTimeout
	}
	return &Queue{
		cfg: cfg,
		sem: make(chan struct{}, cfg.Capacity),
	}
}

// Enqueue schedules a task for retry. It returns false if the queue is saturated.
func (q *Queue) Enqueue(ctx context.Context, task Task) bool {
	if q == nil || task.Do == nil {
		return false
	}
	if task.Operation == "" {
		task.Operation = "persistence"
	}
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = q.cfg.MaxAttempts
	}
	if task.InitialBackoff <= 0 {
		task.InitialBackoff = q.cfg.InitialBackoff
	}
	if task.MaxBackoff <= 0 {
		task.MaxBackoff = q.cfg.MaxBackoff
	}
	if task.AttemptTimeout <= 0 {
		task.AttemptTimeout = q.cfg.AttemptTimeout
	}
	if task.Fields == nil {
		task.Fields = map[string]any{}
	}
	task.Fields = cloneFields(task.Fields)
	task.Fields["queued_at_ms"] = time.Now().UTC().UnixMilli()

	select {
	case q.sem <- struct{}{}:
		go q.run(ctx, task)
		return true
	default:
		return false
	}
}

func (q *Queue) run(ctx context.Context, task Task) {
	defer func() {
		<-q.sem
	}()

	logCtx := ctx
	if logCtx == nil {
		logCtx = context.Background()
	}
	attempts := task.MaxAttempts
	backoff := task.InitialBackoff
	maxBackoff := task.MaxBackoff

	for attempt := 1; attempt <= attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(context.Background(), task.AttemptTimeout)
		err := task.Do(WithRetryContext(attemptCtx))
		cancel()
		if err == nil {
			if attempt > 1 {
				logx.WithContext(logCtx).Infof("persistence retry succeeded op=%s resource=%s attempts=%d fields=%v",
					task.Operation, task.Resource, attempt, task.Fields)
			}
			return
		}

		classification := Classify(err)
		retryable := classification.Class == FailureClassRetryAsync
		if task.Retryable != nil {
			retryable = task.Retryable(err)
		}
		fields := cloneFields(task.Fields)
		fields["attempt"] = attempt
		fields["max_attempts"] = attempts
		fields["failure_class"] = classification.Class
		fields["error"] = err.Error()
		if !retryable || attempt >= attempts {
			logx.WithContext(logCtx).Errorf("persistence retry failed op=%s resource=%s class=%s attempts=%d fields=%v err=%v",
				task.Operation, task.Resource, classification.Class, attempt, fields, err)
			return
		}

		logx.WithContext(logCtx).Slowf("persistence retry scheduled op=%s resource=%s class=%s attempt=%d/%d backoff=%s fields=%v err=%v",
			task.Operation, task.Resource, classification.Class, attempt, attempts, backoff, fields, err)

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(maxBackoff), float64(backoff)*2))
	}
}

// Classify determines whether a persistence error should be blocked, retried, or ignored.
func Classify(err error) Classification {
	if err == nil {
		return Classification{Class: FailureClassIgnore, Reason: "nil"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Classification{Class: FailureClassRetryAsync, Reason: "context timeout"}
	}
	if errors.Is(err, redisv9.Nil) {
		return Classification{Class: FailureClassIgnore, Reason: "cache miss"}
	}
	if retryableTransportError(err) {
		return Classification{Class: FailureClassRetryAsync, Reason: "transient transport"}
	}
	if retryableRedisError(err) {
		return Classification{Class: FailureClassRetryAsync, Reason: "transient redis"}
	}
	return Classification{Class: FailureClassBlock, Reason: "non-transient"}
}

// ShouldRetry reports whether the error should be retried asynchronously.
func ShouldRetry(err error) bool {
	return Classify(err).Class == FailureClassRetryAsync
}

// IsRetryContext marks retry attempts so nested helpers can avoid re-queuing.
type retryContextKey struct{}

// WithRetryContext tags a context as coming from the retry queue.
func WithRetryContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, retryContextKey{}, true)
}

// IsRetryContext reports whether ctx was created by WithRetryContext.
func IsRetryContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(retryContextKey{}).(bool)
	return v
}

func retryableTransportError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "timeout"),
		strings.Contains(text, "i/o timeout"),
		strings.Contains(text, "connection refused"),
		strings.Contains(text, "broken pipe"),
		strings.Contains(text, "connection reset"),
		strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "tls handshake timeout"):
		return true
	default:
		return false
	}
}

func retryableRedisError(err error) bool {
	text := strings.ToUpper(err.Error())
	switch {
	case strings.Contains(text, "LOADING"),
		strings.Contains(text, "BUSY"),
		strings.Contains(text, "TRYAGAIN"),
		strings.Contains(text, "CLUSTERDOWN"),
		strings.Contains(text, "MOVED"),
		strings.Contains(text, "ASK"),
		strings.Contains(text, "READONLY"),
		strings.Contains(text, "MASTERDOWN"),
		strings.Contains(text, "MAX NUMBER OF CLIENTS REACHED"):
		return true
	default:
		return false
	}
}

func cloneFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}
