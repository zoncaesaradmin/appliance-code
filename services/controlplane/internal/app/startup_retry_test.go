package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryStartupOperationWaitsForTransientDependency(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := retryStartupOperation(context.Background(), 4, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("blob storage unavailable")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("retryStartupOperation returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(delays) != 2 || delays[0] != projectionSyncInitial || delays[1] != projectionSyncInitial*2 {
		t.Fatalf("delays = %v, want [%s %s]", delays, projectionSyncInitial, projectionSyncInitial*2)
	}
}

func TestRetryStartupOperationReturnsLastFailure(t *testing.T) {
	want := errors.New("blob storage unavailable")
	attempts := 0
	err := retryStartupOperation(context.Background(), 2, func(context.Context) error {
		attempts++
		return want
	}, func(context.Context, time.Duration) error { return nil }, nil)
	if !errors.Is(err, want) {
		t.Fatalf("retryStartupOperation error = %v, want %v", err, want)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
