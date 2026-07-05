package ctxutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// TraceIDHeader is the header name for trace ID in external message headers.
	TraceIDHeader = "X-Trace-Id"

	// DebugEnabledHeader is the canonical header name for enabling per-flow debug logs.
	DebugEnabledHeader = "X-Enable-Debug"
)

// GenerateTraceID creates a new random trace ID.
func GenerateTraceID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// ExtractTraceID extracts a trace ID from headers, creating one when missing.
func ExtractTraceID(headers map[string]string) string {
	if traceID, exists := headers[TraceIDHeader]; exists && traceID != "" {
		return traceID
	}
	return GenerateTraceID()
}

// ExtractDebugEnabled parses the external per-flow debug flag from headers.
// Missing or invalid values default to false.
func ExtractDebugEnabled(headers map[string]string) bool {
	if len(headers) == 0 {
		return false
	}

	for key, value := range headers {
		switch {
		case strings.EqualFold(key, DebugEnabledHeader):
			return parseDebugEnabled(value)
		case key == "enableDebug", key == "enable_debug", key == "debug":
			return parseDebugEnabled(value)
		}
	}
	return false
}

// BuildFlowContext creates a context carrying both trace ID and per-flow debug settings.
func BuildFlowContext(ctx context.Context, headers map[string]string) (context.Context, string) {
	traceID := ExtractTraceID(headers)
	ctx = WithTraceID(ctx, traceID)
	ctx = WithDebugEnabled(ctx, ExtractDebugEnabled(headers))
	return ctx, traceID
}

// EnsureTraceID ensures the context has a trace ID, creating one if needed.
func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if traceID, ok := GetTraceID(ctx); ok {
		return ctx, traceID
	}

	traceID := GenerateTraceID()
	return WithTraceID(ctx, traceID), traceID
}

// CreateKnownTraceID creates a readable trace ID from a scenario name.
func CreateKnownTraceID(name string) string {
	return cleanForTraceID(name)
}

// CreateScenarioOutlineTraceID creates a unique trace ID for scenario outline examples.
func CreateScenarioOutlineTraceID(scenarioName string, exampleData map[string]string) string {
	baseTraceID := CreateKnownTraceID(scenarioName)

	if len(exampleData) > 0 {
		keys := make([]string, 0, len(exampleData))
		for key := range exampleData {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			cleanKey := cleanForTraceID(key)
			cleanValue := cleanForTraceID(exampleData[key])
			if cleanKey != "" && cleanValue != "" {
				parts = append(parts, cleanKey+"_"+cleanValue)
			}
		}
		if len(parts) > 0 {
			baseTraceID += "_" + strings.Join(parts, "_")
		}
	}

	return baseTraceID + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// CreateContextualTraceID creates an appropriate trace ID based on scenario context.
func CreateContextualTraceID(scenarioName string, exampleData map[string]string) string {
	if len(exampleData) > 0 {
		return CreateScenarioOutlineTraceID(scenarioName, exampleData)
	}
	return CreateKnownTraceID(scenarioName)
}

func cleanForTraceID(input string) string {
	var builder strings.Builder
	for _, char := range input {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		} else if char == ' ' || char == '_' || char == '-' || char == '.' {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func parseDebugEnabled(raw string) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return false
	}

	if parsed, err := strconv.ParseBool(value); err == nil {
		return parsed
	}

	switch value {
	case "1", "y", "yes", "on", "debug":
		return true
	default:
		return false
	}
}
