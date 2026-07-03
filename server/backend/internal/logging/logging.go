// Package logging builds the process-wide logger on top of the shared
// platformkit/logging package (zerolog-backed, JSON output) used across all
// sibling repos, and layers on redaction of known-sensitive field keys so
// credentials never reach log output, per the plan's "no plaintext secrets
// in logs" requirement. platformkit does not redact fields itself, so that
// responsibility stays local to this package.
package logging

import (
	"context"
	"io"
	"os"
	"strings"

	platformlogging "platformkit/logging"
)

// Logger is the shared logging.Logger interface every appliance-code
// package should depend on, rather than importing platformkit directly.
type Logger = platformlogging.Logger

// New builds a redacted structured logger writing to stdout at the given
// level ("debug", "info", "warn", or "error").
func New(level string) (Logger, error) {
	return NewWithWriter(level, os.Stdout)
}

// NewWithWriter builds a redacted structured logger writing to w. It exists
// separately from New so tests can assert on redaction and context behavior
// against an in-memory buffer instead of stdout.
func NewWithWriter(level string, w io.Writer) (Logger, error) {
	base, err := platformlogging.NewLogger(&platformlogging.LoggerConfig{
		Level:         platformlogging.ConvertToLogLevel(level),
		LoggerName:    "appliance-server",
		ComponentName: "control-plane",
		ServiceName:   "appliance-server",
		OutputWriter:  w,
	})
	if err != nil {
		return nil, err
	}
	return &redactingLogger{Logger: base}, nil
}

// sensitiveKeys are field names redacted regardless of nesting depth.
// Matching is case-insensitive and matches on substring so variants like
// "AuthorizationHeader" or "user_password" are also caught.
var sensitiveKeys = []string{
	"password",
	"secret",
	"token",
	"authorization",
	"apikey",
	"api_key",
	"cookie",
	"refresh",
}

const redacted = "[REDACTED]"

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// RedactMap returns a copy of fields with every sensitive-looking key's
// value replaced, for use by any package (such as internal/audit) that
// builds structured detail payloads outside of a Logger call.
func RedactMap(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if isSensitiveKey(k) {
			out[k] = redacted
			continue
		}
		out[k] = v
	}
	return out
}

func redactFields(fields platformlogging.Fields) platformlogging.Fields {
	out := make(platformlogging.Fields, len(fields))
	for k, v := range fields {
		if isSensitiveKey(k) {
			out[k] = redacted
			continue
		}
		out[k] = v
	}
	return out
}

func redactKeysAndValues(keysAndValues []interface{}) []interface{} {
	out := make([]interface{}, len(keysAndValues))
	copy(out, keysAndValues)
	for i := 0; i+1 < len(out); i += 2 {
		if key, ok := out[i].(string); ok && isSensitiveKey(key) {
			out[i+1] = redacted
		}
	}
	return out
}

// redactingLogger wraps a platformkit Logger, redacting sensitive field keys
// on every entry point that accepts structured fields. Methods that only
// accept a plain message (Debug, Info, Warn, Error, Fatal, Panic, and their
// f-variants) pass straight through, matching platformkit's own contract.
type redactingLogger struct {
	platformlogging.Logger
}

func wrap(l platformlogging.Logger) platformlogging.Logger {
	if l == nil {
		return nil
	}
	return &redactingLogger{Logger: l}
}

func (r *redactingLogger) WithFields(fields platformlogging.Fields) platformlogging.Logger {
	return wrap(r.Logger.WithFields(redactFields(fields)))
}

func (r *redactingLogger) WithField(key string, value interface{}) platformlogging.Logger {
	return r.WithFields(platformlogging.Fields{key: value})
}

func (r *redactingLogger) WithContext(ctx context.Context) platformlogging.Logger {
	return wrap(r.Logger.WithContext(ctx))
}

func (r *redactingLogger) Clone() platformlogging.Logger {
	return wrap(r.Logger.Clone())
}

func (r *redactingLogger) Debugw(msg string, keysAndValues ...interface{}) {
	r.Logger.Debugw(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Infow(msg string, keysAndValues ...interface{}) {
	r.Logger.Infow(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Warnw(msg string, keysAndValues ...interface{}) {
	r.Logger.Warnw(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Errorw(msg string, keysAndValues ...interface{}) {
	r.Logger.Errorw(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	r.Logger.Fatalw(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Panicw(msg string, keysAndValues ...interface{}) {
	r.Logger.Panicw(msg, redactKeysAndValues(keysAndValues)...)
}

func (r *redactingLogger) Logw(level platformlogging.Level, msg string, keysAndValues ...interface{}) {
	r.Logger.Logw(level, msg, redactKeysAndValues(keysAndValues)...)
}
