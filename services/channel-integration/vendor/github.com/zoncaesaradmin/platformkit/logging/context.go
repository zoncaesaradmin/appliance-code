package logging

import (
	"context"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

// WithDebugEnabled stores a per-flow debug override in the context.
// When enabled, loggers using this context honor Debug-level logs for that flow
// without changing the process-wide logger level.
func WithDebugEnabled(ctx context.Context, enabled bool) context.Context {
	return ctxutil.WithDebugEnabled(ctx, enabled)
}

// GetDebugEnabled returns the per-flow debug override from the context.
func GetDebugEnabled(ctx context.Context) (bool, bool) {
	return ctxutil.GetDebugEnabled(ctx)
}

func effectiveLevel(base Level, ctx context.Context) Level {
	if enabled, ok := ctxutil.GetDebugEnabled(ctx); ok && enabled {
		return DebugLevel
	}
	return base
}
