package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func handler() http.Handler {
	ready := &atomic.Bool{}
	ready.Store(true)
	return authHandler{ready: ready}
}

func TestHandlerHealthAndCapabilities(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "live", path: "/health/live", want: http.StatusOK},
		{name: "ready", path: "/health/ready", want: http.StatusOK},
		{name: "capabilities", path: "/api/v1/auth/capabilities", want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			res := httptest.NewRecorder()
			handler().ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
		})
	}
}

func TestLoadConfigDefaultsAddress(t *testing.T) {
	get := func(name string) string {
		switch name {
		case "AUTH_SERVICE_DATABASE_URL":
			return "postgres://example"
		case "AUTH_SERVICE_ORIGIN":
			return "https://app.example.com"
		case "AUTH_SERVICE_OTP_KEY", "AUTH_SERVICE_RATE_KEY":
			return "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2t="
		default:
			return ""
		}
	}
	config, err := loadConfig(get)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.address; got != ":8080" {
		t.Fatalf("address = %q, want :8080", got)
	}
}
