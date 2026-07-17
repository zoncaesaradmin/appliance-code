package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zoncaesaradmin/platformkit/ctxutil"

	"appliance-code/services/controlplane/internal/logging"
)

const requestIDHeader = "X-Request-Id"

// requestIDFromRequest reads the request ID stashed on the request context
// by the RequestID middleware, used by problem responses so every error
// carries a correlation ID.
func requestIDFromRequest(r *http.Request) string {
	id, _ := ctxutil.GetRequestID(r.Context())
	return id
}

// RequestID assigns a UUIDv7 request ID to every request that doesn't
// already carry a trusted one, propagates it on the response header, and
// stores it via platformkit/ctxutil so downstream logger.WithContext calls
// pick it up automatically, matching the convention used across all sibling
// repos.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = uuid.Must(uuid.NewV7()).String()
		}
		w.Header().Set(requestIDHeader, id)

		ctx := ctxutil.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recover converts a panic in any downstream handler into a 500
// application/problem+json response instead of crashing the process or
// leaking a stack trace to the client.
func Recover(logger logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.WithContext(r.Context()).Errorw("panic recovered",
						"panic", rec,
						"method", r.Method,
						"path", r.URL.Path,
					)
					WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the status code written by downstream handlers so
// the access-log middleware can report it after the handler returns.
type statusRecorder struct {
	http.ResponseWriter
	status    int
	body      bytes.Buffer
	truncated bool
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if !s.truncated {
		remaining := traceBodyLimit - s.body.Len()
		if remaining > 0 {
			if len(p) > remaining {
				_, _ = s.body.Write(p[:remaining])
				s.truncated = true
			} else {
				_, _ = s.body.Write(p)
			}
		} else {
			s.truncated = true
		}
	}
	return s.ResponseWriter.Write(p)
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (s *statusRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := s.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

// AccessLog logs one line per request with method, path, status, duration,
// and request ID at info level so operators can confirm that the control
// plane received and answered a request without needing a debug-only build or
// log-level override.
func AccessLog(logger logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if logger == nil {
				return
			}
			logger.WithContext(r.Context()).Infow("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start).String(),
			)
		})
	}
}

const traceBodyLimit = 4 * 1024

// APIExchangeLog records the redacted request and response summary for public
// REST API calls so operators can confirm exactly what reached the control
// plane and what it returned, without relying on browser-visible UI redirects.
func APIExchangeLog(logger logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil || !shouldTraceAPIExchange(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			requestBody := cloneRequestBody(r)
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			logger.WithContext(r.Context()).Infow("http api exchange",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start).String(),
				"request", summarizeBodyForLog(requestBody),
				"response", summarizeBodyForLog(rec.body.Bytes()),
				"responseTruncated", rec.truncated,
			)
		})
	}
}

func shouldTraceAPIExchange(path string) bool {
	return strings.HasPrefix(path, "/api/v1/")
}

func cloneRequestBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func summarizeBodyForLog(body []byte) any {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	if len(body) > traceBodyLimit {
		return map[string]any{
			"size":      len(body),
			"truncated": true,
		}
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err == nil {
		return redactJSONValue(decoded)
	}

	limited := limitBytes(body, 1024)
	return map[string]any{
		"raw":       string(limited),
		"truncated": len(limited) < len(body),
	}
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveTraceKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactJSONValue(child)
		}
		return out
	default:
		return value
	}
}

func isSensitiveTraceKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range []string{"password", "token", "authorization", "secret", "privatekey", "private_key", "credential", "refresh", "cookie"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func limitBytes(body []byte, max int) []byte {
	if len(body) <= max {
		return body
	}
	return body[:max]
}

// Chain composes middleware in the order given, so Chain(a, b)(handler) runs
// a, then b, then handler.
func Chain(mw ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			final = mw[i](final)
		}
		return final
	}
}
