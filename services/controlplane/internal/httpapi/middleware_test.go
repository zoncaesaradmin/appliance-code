package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
