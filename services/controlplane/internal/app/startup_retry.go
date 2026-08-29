package app

import (
	"context"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
)

const (
	projectionSyncAttempts = 12
	projectionSyncInitial  = 250 * time.Millisecond
	projectionSyncMaximum  = 5 * time.Second
)

// syncVideoProjection waits for the appliance-owned blob service during a
// cold start. A permanent storage fault still prevents startup; transient
// service ordering does not consume Kubernetes restart attempts.
func syncVideoProjection(ctx context.Context, handler *httpapi.VideoLibraryHandlers, logger logging.Logger) error {
	return retryStartupOperation(ctx, projectionSyncAttempts, handler.SyncProjection, waitForStartupRetry, func(err error, delay time.Duration) {
		logger.Warnw("video media projection is waiting for blob storage", "error", err, "retryAfter", delay)
	})
}

func retryStartupOperation(ctx context.Context, attempts int, operation func(context.Context) error, wait func(context.Context, time.Duration) error, onRetry func(error, time.Duration)) error {
	if attempts < 1 {
		return fmt.Errorf("startup retry attempts must be positive")
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := operation(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == attempts-1 {
			break
		}
		delay := projectionSyncInitial << attempt
		if delay > projectionSyncMaximum {
			delay = projectionSyncMaximum
		}
		if onRetry != nil {
			onRetry(lastErr, delay)
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
	return lastErr
}

func waitForStartupRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
