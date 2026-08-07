package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/logging"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

func TestTraceIDPropagatesFromHeader(t *testing.T) {
	handler := Chain(TraceID)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, ok := ctxutil.GetTraceID(r.Context())
		if !ok {
			t.Fatal("trace ID missing from context")
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(traceID))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set(ctxutil.TraceIDHeader, "trace-controlplane-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(ctxutil.TraceIDHeader); got != "trace-controlplane-123" {
		t.Fatalf("response trace header = %q, want trace-controlplane-123", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "trace-controlplane-123" {
		t.Fatalf("trace body = %q, want trace-controlplane-123", got)
	}
}

func TestAPIExchangeLogRedactsRequestAndResponse(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	handler := Chain(TraceID, RequestID, APIExchangeLog(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/current-workspace" {
			t.Fatalf("path = %s, want /api/v1/current-workspace", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workspaceId":"ws_demo","accessToken":"secret-access"}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/current-workspace", strings.NewReader(`{"workspaceId":"ws_demo","password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ctxutil.TraceIDHeader, "trace-controlplane-456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	record := findLogRecord(t, logBuf.String(), "http api exchange")
	if got := record["path"]; got != "/api/v1/current-workspace" {
		t.Fatalf("path = %#v, want /api/v1/current-workspace", got)
	}
	if got := int(record["status"].(float64)); got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := record["requestId"]; got == "" {
		t.Fatalf("requestId = %#v, want non-empty", got)
	}
	if got := record["traceId"]; got != "trace-controlplane-456" {
		t.Fatalf("traceId = %#v, want trace-controlplane-456", got)
	}

	request, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %#v, want object", record["request"])
	}
	if got := request["workspaceId"]; got != "ws_demo" {
		t.Fatalf("request.workspaceId = %#v, want ws_demo", got)
	}
	if got := request["password"]; got != "[REDACTED]" {
		t.Fatalf("request.password = %#v, want [REDACTED]", got)
	}

	response, ok := record["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want object", record["response"])
	}
	if got := response["workspaceId"]; got != "ws_demo" {
		t.Fatalf("response.workspaceId = %#v, want ws_demo", got)
	}
	if got := response["accessToken"]; got != "[REDACTED]" {
		t.Fatalf("response.accessToken = %#v, want [REDACTED]", got)
	}
}

func TestAPIExchangeLogDoesNotBufferLargeUploadBodies(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	const uploadSize = 8 * 1024 * 1024
	payload := bytes.Repeat([]byte("a"), uploadSize)
	var received int
	handler := Chain(TraceID, RequestID, APIExchangeLog(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			t.Fatalf("handler read body: %v", err)
		}
		received = int(n)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"appliance/probe/large.bin","size":8388608}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/appliance/probe/large.bin", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(uploadSize)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if received != uploadSize {
		t.Fatalf("handler received %d bytes, want %d (body must stream, not be drained by exchange log)", received, uploadSize)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	record := findLogRecord(t, logBuf.String(), "http api exchange")
	request, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %#v, want truncated size object", record["request"])
	}
	if got := int64(request["size"].(float64)); got != int64(uploadSize) {
		t.Fatalf("request.size = %d, want %d", got, uploadSize)
	}
	if truncated, _ := request["truncated"].(bool); !truncated {
		t.Fatalf("request.truncated = %#v, want true", request["truncated"])
	}
}

func TestStatusRecorderUnwrapExposesUnderlyingWriter(t *testing.T) {
	inner := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}
	if got := wrapped.Unwrap(); got != inner {
		t.Fatalf("Unwrap() = %T, want underlying ResponseRecorder", got)
	}

	logger := mustLogger(t)
	handler := Chain(APIExchangeLog(logger), AccessLog(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctrl := http.NewResponseController(w)
		// httptest recorder does not support deadlines; ErrNotSupported proves
		// ResponseController walked past statusRecorder via Unwrap.
		err := ctrl.SetReadDeadline(time.Time{})
		if err == nil {
			t.Fatal("expected ErrNotSupported from httptest recorder")
		}
		if !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("SetReadDeadline error = %v, want ErrNotSupported (Unwrap broken?)", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/probe.bin", nil)
	handler.ServeHTTP(inner, req)
}

func mustLogger(t *testing.T) logging.Logger {
	t.Helper()
	var buf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	return logger
}

func TestAccessLogSuppressesPublicAPIRequests(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	handler := Chain(TraceID, RequestID, AccessLog(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/work-profiles", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if strings.TrimSpace(logBuf.String()) != "" {
		t.Fatalf("expected no access log for public API request, got %s", logBuf.String())
	}
}

func TestAccessLogKeepsNonAPIRequests(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	handler := Chain(TraceID, RequestID, AccessLog(logger))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/internal/auth/check", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	record := findLogRecord(t, logBuf.String(), "http request")
	if got := record["path"]; got != "/internal/auth/check" {
		t.Fatalf("path = %#v, want /internal/auth/check", got)
	}
}

func findLogRecord(t *testing.T, text, message string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse log JSON: %v\nlog=%s", err, line)
		}
		if record["message"] == message || record["msg"] == message {
			return record
		}
	}
	t.Fatalf("did not find log message %q in %s", message, text)
	return nil
}
