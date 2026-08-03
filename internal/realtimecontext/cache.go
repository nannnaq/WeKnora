package realtimecontext

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type cachedSource[T any] struct {
	mu          sync.RWMutex
	value       *T
	lastSuccess time.Time
	group       singleflight.Group
}

func (c *cachedSource[T]) peek() (T, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.value == nil {
		var zero T
		return zero, time.Time{}, false
	}
	return *c.value, c.lastSuccess, true
}

func (c *cachedSource[T]) load(
	ctx context.Context,
	ttl time.Duration,
	refresh func(context.Context) (T, error),
) (T, Freshness, time.Time, error) {
	if current, asOf, ok := c.peek(); ok && time.Since(asOf) < ttl {
		return current, FreshnessFresh, asOf, nil
	}

	value, err, _ := c.group.Do("refresh", func() (any, error) {
		if current, asOf, ok := c.peek(); ok && time.Since(asOf) < ttl {
			return cachedLoadResult[T]{value: current, asOf: asOf}, nil
		}
		current, refreshErr := refresh(ctx)
		if refreshErr != nil {
			return nil, refreshErr
		}
		now := time.Now()
		c.mu.Lock()
		c.value = &current
		c.lastSuccess = now
		c.mu.Unlock()
		return cachedLoadResult[T]{value: current, asOf: now}, nil
	})
	if err == nil {
		loaded := value.(cachedLoadResult[T])
		return loaded.value, FreshnessFresh, loaded.asOf, nil
	}
	if stale, asOf, ok := c.peek(); ok {
		return stale, FreshnessStale, asOf, err
	}
	var zero T
	return zero, FreshnessUnavailable, time.Time{}, err
}

type cachedLoadResult[T any] struct {
	value T
	asOf  time.Time
}
