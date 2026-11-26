package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	if got := loadConfig(func(string) string { return "" }).address; got != ":8080" {
		t.Fatalf("address = %q, want :8080", got)
	}
}
