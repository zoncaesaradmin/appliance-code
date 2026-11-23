package identity

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalRequestLimiter(t *testing.T) {
	if _, err := NewLocalRequestLimiter("production", time.Minute, map[string]int{"verify": 1}, 10); err == nil {
		t.Fatal("production must reject local limiter")
	}
	limits := map[string]int{"verify": 2, "challenge": 1}
	l, err := NewLocalRequestLimiter("test", time.Minute, limits, 2)
	if err != nil {
		t.Fatal(err)
	}
	limits["verify"] = 100 // caller must not be able to change configured limits
	now := time.Now()
	l.now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if ok, err := l.Allow(ctx, "client", "verify"); !ok || err != nil {
			t.Fatalf("attempt %d: %v %v", i, ok, err)
		}
	}
	if ok, err := l.Allow(ctx, "client", "verify"); ok || err != nil {
		t.Fatalf("rate limit: %v %v", ok, err)
	}
	if ok, err := l.Allow(ctx, "client", "challenge"); !ok || err != nil {
		t.Fatalf("independent action: %v %v", ok, err)
	}
	if _, err := l.Allow(ctx, "another", "verify"); err == nil {
		t.Fatal("capacity must fail closed")
	}
	now = now.Add(time.Minute)
	if ok, err := l.Allow(ctx, "another", "verify"); !ok || err != nil {
		t.Fatalf("expired buckets: %v %v", ok, err)
	}
	if _, err := l.Allow(ctx, "client", "unknown"); err == nil {
		t.Fatal("unknown action accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := l.Allow(cancelled, "client", "verify"); err == nil {
		t.Fatal("cancelled context accepted")
	}
}

func TestLocalRequestLimiterConcurrentLimit(t *testing.T) {
	l, err := NewLocalRequestLimiter("test", time.Minute, map[string]int{"verify": 5}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := l.Allow(context.Background(), "same", "verify"); err != nil {
				t.Error(err)
			} else if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d requests, want 5", allowed.Load())
	}
}
