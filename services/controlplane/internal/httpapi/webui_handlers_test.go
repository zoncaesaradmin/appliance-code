package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebUIReadinessUsesSessionBridgeHealthEndpoint(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer up.Close()
	h := &WebUIHandlers{HealthURL: up.URL, Client: up.Client()}
	if h.ready(httptest.NewRequest(http.MethodPost, "/api/v1/webui/launch", nil)) {
		t.Fatal("unready session bridge accepted")
	}

	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()
	h.HealthURL, h.Client = ready.URL, ready.Client()
	if !h.ready(httptest.NewRequest(http.MethodPost, "/api/v1/webui/launch", nil)) {
		t.Fatal("ready session bridge rejected")
	}
}
