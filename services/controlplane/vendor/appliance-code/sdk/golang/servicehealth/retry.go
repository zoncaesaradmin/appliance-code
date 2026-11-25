// Package servicehealth provides the shared lifecycle primitives used by
// appliance services that depend on other runtime components.
package servicehealth

import (
	"context"
	"errors"
	"time"
)

// RetryPolicy waits for a dependency without turning a temporary outage into
// a container restart. Delays grow exponentially until MaxDelay, then remain
// capped at that interval.
type RetryPolicy struct {
	InitialDelay   time.Duration
	MaxDelay       time.Duration
	AttemptTimeout time.Duration
}

// Wait invokes attempt until it succeeds or ctx is cancelled. The callback is
// called after each failed attempt and before the next sleep.
func (p RetryPolicy) Wait(ctx context.Context, attempt func(context.Context) error, onRetry func(error, time.Duration)) error {
	if attempt == nil {
		return errors.New("servicehealth: retry attempt is required")
	}
	if ctx == nil {
		return errors.New("servicehealth: retry context is required")
	}
	policy := p.normalized()
	delay := policy.InitialDelay
	for {
		attemptCtx := ctx
		cancel := func() {}
		if policy.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, policy.AttemptTimeout)
		}
		err := attempt(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if onRetry != nil {
			onRetry(err, delay)
		}
		if err := Sleep(ctx, delay); err != nil {
			return err
		}
		delay = nextDelay(delay, policy.MaxDelay)
	}
}

// Sleep waits for delay or cancellation of ctx.
func Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.InitialDelay <= 0 {
		p.InitialDelay = time.Second
	}
	if p.MaxDelay < p.InitialDelay {
		p.MaxDelay = p.InitialDelay
	}
	return p
}

func nextDelay(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next < current || next > maximum {
		return maximum
	}
	return next
}
