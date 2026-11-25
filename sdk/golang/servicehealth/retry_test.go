package servicehealth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyWaitsUntilSuccess(t *testing.T) {
	attempts := 0
	var retries []time.Duration
	err := (RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: 4 * time.Millisecond}).Wait(
		context.Background(),
		func(context.Context) error {
			attempts++
			if attempts == 3 {
				return nil
			}
			return errors.New("not ready")
		},
		func(_ error, delay time.Duration) { retries = append(retries, delay) },
	)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	want := []time.Duration{time.Millisecond, 2 * time.Millisecond}
	for i, delay := range want {
		if retries[i] != delay {
			t.Errorf("retry delay[%d] = %s, want %s", i, retries[i], delay)
		}
	}
}

func TestRetryPolicyStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := (RetryPolicy{InitialDelay: time.Hour}).Wait(
		ctx,
		func(context.Context) error { return errors.New("not ready") },
		func(error, time.Duration) { cancel() },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
}

func TestRetryPolicyCapsDelay(t *testing.T) {
	policy := RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: 4 * time.Millisecond}.normalized()
	delay := policy.InitialDelay
	for i := 0; i < 10; i++ {
		delay = nextDelay(delay, policy.MaxDelay)
	}
	if delay != policy.MaxDelay {
		t.Fatalf("delay = %s, want %s", delay, policy.MaxDelay)
	}
}
