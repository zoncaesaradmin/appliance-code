package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	uilogging "appliance-code/services/ui/internal/logging"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTraceLogsWorkspaceRequestAndResponse(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/current-workspace" {
			t.Fatalf("got %s %s, want POST /api/v1/current-workspace", r.Method, r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"ws_demo","name":"Demo","workProfile":"platform-dev","status":"ready","createdAt":"2026-07-17T00:00:00Z","updatedAt":"2026-07-17T00:00:00Z"}`)),
		}, nil
	})}

	var logBuf bytes.Buffer
	logger, err := uilogging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	client := NewClient(Config{
		BaseURL:    "http://control-plane.test",
		HTTPClient: clientHTTP,
		Logger:     logger,
		TraceHTTP:  true,
	})

	if _, err := client.SetCurrentWorkspace(context.Background(), "access-token", "ws_demo"); err != nil {
		t.Fatalf("SetCurrentWorkspace: %v", err)
	}

	record := parseSingleJSONLogLine(t, logBuf.String())
	if got := record["path"]; got != "/api/v1/current-workspace" {
		t.Fatalf("path = %#v, want /api/v1/current-workspace", got)
	}
	if got := int(record["status"].(float64)); got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	request, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %#v, want object", record["request"])
	}
	if got := request["workspaceId"]; got != "ws_demo" {
		t.Fatalf("request.workspaceId = %#v, want ws_demo", got)
	}
	response, ok := record["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want object", record["response"])
	}
	if got := response["id"]; got != "ws_demo" {
		t.Fatalf("response.id = %#v, want ws_demo", got)
	}
}

func TestTraceRedactsSensitiveLoginFields(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("got %s %s, want POST /api/v1/auth/login", r.Method, r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"accessToken":"secret-access","refreshToken":"secret-refresh","accessExpiresAt":"2026-07-17T00:15:00Z"}`)),
		}, nil
	})}

	var logBuf bytes.Buffer
	logger, err := uilogging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	client := NewClient(Config{
		BaseURL:    "http://control-plane.test",
		HTTPClient: clientHTTP,
		Logger:     logger,
		TraceHTTP:  true,
	})

	if _, err := client.Login(context.Background(), "admin", "super-secret"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	record := parseSingleJSONLogLine(t, logBuf.String())
	request, ok := record["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %#v, want object", record["request"])
	}
	if got := request["username"]; got != "admin" {
		t.Fatalf("request.username = %#v, want admin", got)
	}
	if got := request["password"]; got != "[redacted]" {
		t.Fatalf("request.password = %#v, want [redacted]", got)
	}
	response, ok := record["response"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want object", record["response"])
	}
	if got := response["accessToken"]; got != "[redacted]" {
		t.Fatalf("response.accessToken = %#v, want [redacted]", got)
	}
	if got := response["refreshToken"]; got != "[redacted]" {
		t.Fatalf("response.refreshToken = %#v, want [redacted]", got)
	}
}

func parseSingleJSONLogLine(t *testing.T, text string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(text)
	if line == "" {
		t.Fatal("expected one JSON log line, got empty output")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("parse log JSON: %v\nlog=%s", err, line)
	}
	return record
}
