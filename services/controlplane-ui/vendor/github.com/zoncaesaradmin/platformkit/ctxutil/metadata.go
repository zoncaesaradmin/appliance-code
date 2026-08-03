package ctxutil

import "context"

// WithField stores a custom metadata field in the context.
func WithField(ctx context.Context, key string, value interface{}) context.Context {
	return WithFields(ctx, map[string]interface{}{key: value})
}

// WithFields stores custom metadata fields in the context.
func WithFields(ctx context.Context, fields map[string]interface{}) context.Context {
	return withValue(ctx, fieldsKey, mergeFields(GetFields(ctx), fields))
}

// GetFields returns the custom metadata fields stored in the context.
func GetFields(ctx context.Context) map[string]interface{} {
	if ctx == nil {
		return map[string]interface{}{}
	}
	fields, ok := ctx.Value(fieldsKey).(map[string]interface{})
	if !ok || len(fields) == 0 {
		return map[string]interface{}{}
	}
	return mergeFields(nil, fields)
}

// FieldsFromContext extracts all standardized fields plus custom metadata fields.
func FieldsFromContext(ctx context.Context) map[string]interface{} {
	fields := GetFields(ctx)

	if traceID, ok := GetTraceID(ctx); ok && traceID != "" {
		fields["traceId"] = traceID
	}
	if requestID, ok := GetRequestID(ctx); ok && requestID != "" {
		fields["requestId"] = requestID
	}
	if correlationID, ok := GetCorrelationID(ctx); ok && correlationID != "" {
		fields["correlationId"] = correlationID
	}
	if userID, ok := GetUserID(ctx); ok && userID != "" {
		fields["userId"] = userID
	}
	if tenantID, ok := GetTenantID(ctx); ok && tenantID != "" {
		fields["tenantId"] = tenantID
	}
	if debugEnabled, ok := GetDebugEnabled(ctx); ok && debugEnabled {
		fields["debugEnabled"] = true
	}

	return fields
}

func mergeFields(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
