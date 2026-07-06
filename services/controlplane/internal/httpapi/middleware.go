package httpapi

import (
	"net/http"
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
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// AccessLog logs one line per request with method, path, status, duration,
// and request ID, at debug level to avoid flooding production logs with
// routine traffic by default.
func AccessLog(logger logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.WithContext(r.Context()).Debugw("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start).String(),
			)
		})
	}
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
