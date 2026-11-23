package identity

import (
	"context"
	"errors"
	"sync"
	"time"
)

type localBucketKey struct{ client, action string }

// LocalRequestLimiter is process-local and only suitable for local/test use.
// A production deployment needs a shared, independently operated limit.
type LocalRequestLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	limits   map[string]int
	capacity int
	buckets  map[localBucketKey][]time.Time
	now      func() time.Time
}

func NewLocalRequestLimiter(environment string, window time.Duration, limits map[string]int, capacity int) (*LocalRequestLimiter, error) {
	if (environment != "local" && environment != "test") || window <= 0 || window > time.Hour || capacity <= 0 || capacity > 100000 || len(limits) == 0 {
		return nil, errors.New("invalid local request limiter configuration")
	}
	copied := make(map[string]int, len(limits))
	for action, limit := range limits {
		if action == "" || len(action) > 256 || limit <= 0 || limit > 1000 {
			return nil, errors.New("invalid local request limit")
		}
		copied[action] = limit
	}
	return &LocalRequestLimiter{window: window, limits: copied, capacity: capacity, buckets: make(map[localBucketKey][]time.Time), now: time.Now}, nil
}

func (l *LocalRequestLimiter) Allow(ctx context.Context, client, action string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	limit, ok := l.limits[action]
	if !ok || client == "" || len(client) > 256 {
		return false, errors.New("invalid request limit key")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := l.now()
	cutoff := now.Add(-l.window)
	key := localBucketKey{client: client, action: action}
	times := l.buckets[key]
	first := 0
	for first < len(times) && !times[first].After(cutoff) {
		first++
	}
	times = times[first:]
	if len(times) >= limit {
		l.buckets[key] = times
		return false, nil
	}
	if len(times) == 0 {
		// Remove expired buckets only when a new bucket needs space.
		if len(l.buckets) >= l.capacity {
			for k, entries := range l.buckets {
				if len(entries) == 0 || !entries[len(entries)-1].After(cutoff) {
					delete(l.buckets, k)
				}
			}
		}
		if _, exists := l.buckets[key]; !exists && len(l.buckets) >= l.capacity {
			return false, errors.New("local request limiter capacity exhausted")
		}
	}
	l.buckets[key] = append(times, now)
	return true, nil
}
