package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootstrapHealthHandler(t *testing.T) {
	handler := bootstrapHealthHandler()
	tests := []struct {
		path string
		want int
	}{
		{path: "/health/live", want: http.StatusOK},
		{path: "/health/startup", want: http.StatusServiceUnavailable},
		{path: "/health/ready", want: http.StatusServiceUnavailable},
		{path: "/api/v1/users", want: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != test.want {
				t.Fatalf("status = %d, want %d", res.Code, test.want)
			}
		})
	}
}
