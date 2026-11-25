package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryUntilReadyRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	started := make(chan struct{})

	err := retryUntilReady(
		context.Background(),
		func(context.Context) error {
			attempts++
			if attempts == 3 {
				return nil
			}
			return errors.New("database unavailable")
		},
		func(ctx context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			if len(delays) == 2 {
				close(started)
			}
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("retryUntilReady() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	select {
	case <-started:
	default:
		t.Fatal("retry sleeper was not called twice")
	}
	want := []time.Duration{databaseRetryInitialDelay, 2 * databaseRetryInitialDelay}
	for i := range want {
		if delays[i] != want[i] {
			t.Errorf("delay[%d] = %s, want %s", i, delays[i], want[i])
		}
	}
}

func TestRetryUntilReadyStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	err := retryUntilReady(
		ctx,
		func(context.Context) error {
			attempts++
			return errors.New("database unavailable")
		},
		func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryUntilReady() error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestNextRetryDelayCapsAtMaximum(t *testing.T) {
	delay := databaseRetryInitialDelay
	for i := 0; i < 10; i++ {
		delay = nextRetryDelay(delay)
	}
	if delay != databaseRetryMaxDelay {
		t.Fatalf("delay = %s, want %s", delay, databaseRetryMaxDelay)
	}
}
