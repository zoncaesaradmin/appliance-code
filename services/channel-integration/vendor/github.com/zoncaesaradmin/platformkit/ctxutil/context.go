package ctxutil

import (
	"context"
	"strconv"
)

// Key is the typed context key used by the ctxutil package.
type Key string

const (
	TraceIDKey       Key = "ctxutil.traceId"
	RequestIDKey     Key = "ctxutil.requestId"
	CorrelationIDKey Key = "ctxutil.correlationId"
	UserIDKey        Key = "ctxutil.userId"
	TenantIDKey      Key = "ctxutil.tenantId"
	DebugEnabledKey  Key = "ctxutil.debugEnabled"
	fieldsKey        Key = "ctxutil.fields"
)

// WithTraceID stores a trace ID in the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return withValue(ctx, TraceIDKey, traceID)
}

// GetTraceID reads a trace ID from the context.
func GetTraceID(ctx context.Context) (string, bool) {
	return lookupString(ctx, TraceIDKey, "traceId", "traceID", "trace_id")
}

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return withValue(ctx, RequestIDKey, requestID)
}

// GetRequestID reads a request ID from the context.
func GetRequestID(ctx context.Context) (string, bool) {
	return lookupString(ctx, RequestIDKey, "requestId", "requestID", "request_id")
}

// WithCorrelationID stores a correlation ID in the context.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return withValue(ctx, CorrelationIDKey, correlationID)
}

// GetCorrelationID reads a correlation ID from the context.
func GetCorrelationID(ctx context.Context) (string, bool) {
	return lookupString(ctx, CorrelationIDKey, "correlationId", "correlationID", "correlation_id")
}

// WithUserID stores a user ID in the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return withValue(ctx, UserIDKey, userID)
}

// GetUserID reads a user ID from the context.
func GetUserID(ctx context.Context) (string, bool) {
	return lookupString(ctx, UserIDKey, "userId", "userID", "user_id")
}

// WithTenantID stores a tenant ID in the context.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return withValue(ctx, TenantIDKey, tenantID)
}

// GetTenantID reads a tenant ID from the context.
func GetTenantID(ctx context.Context) (string, bool) {
	return lookupString(ctx, TenantIDKey, "tenantId", "tenantID", "tenant_id")
}

// WithDebugEnabled stores a per-flow debug override in the context.
func WithDebugEnabled(ctx context.Context, enabled bool) context.Context {
	return withValue(ctx, DebugEnabledKey, enabled)
}

// GetDebugEnabled reads a per-flow debug override from the context.
func GetDebugEnabled(ctx context.Context) (bool, bool) {
	return lookupBool(ctx, DebugEnabledKey, "debugEnabled", "enableDebug", "enable_debug")
}

func withValue(ctx context.Context, key Key, value interface{}) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, key, value)
}

func lookupString(ctx context.Context, keys ...interface{}) (string, bool) {
	for _, key := range keys {
		if value, ok := lookupContextValue(ctx, key); ok {
			if str, ok := value.(string); ok && str != "" {
				return str, true
			}
		}
	}
	return "", false
}

func lookupBool(ctx context.Context, keys ...interface{}) (bool, bool) {
	for _, key := range keys {
		if value, ok := lookupContextValue(ctx, key); ok {
			switch typed := value.(type) {
			case bool:
				return typed, true
			case string:
				parsed, err := strconv.ParseBool(typed)
				if err == nil {
					return parsed, true
				}
			}
		}
	}
	return false, false
}

func lookupContextValue(ctx context.Context, key interface{}) (interface{}, bool) {
	if ctx == nil {
		return nil, false
	}
	value := ctx.Value(key)
	if value == nil {
		return nil, false
	}
	return value, true
}
