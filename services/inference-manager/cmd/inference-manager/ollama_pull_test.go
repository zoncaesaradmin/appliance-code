package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOllamaPullReportsStreamProgressAndRequiresSuccess(t *testing.T) {
	progressSeen := make(chan struct{})
	continuePull := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "qwen2.5-coder:7b" || !body.Stream {
			t.Errorf("pull request: body=%+v err=%v", body, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"status\":\"downloading digest\",\"digest\":\"sha256:one\",\"total\":100,\"completed\":40}\n"))
		w.(http.Flusher).Flush()
		close(progressSeen)
		<-continuePull
		_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer backend.Close()
	m := testManager(t)
	m.engine = "ollama"
	m.backend, _ = url.Parse(backend.URL)
	m.beginImportProgress("qwen2.5-coder:7b", "qwen2.5-coder:7b", 100, "Pulling model")
	done := make(chan error, 1)
	go func() { done <- m.pullOllamaModel(context.Background(), "qwen2.5-coder:7b", time.Second) }()
	<-progressSeen
	deadline := time.Now().Add(time.Second)
	for m.currentImportProgress().BytesDownloaded != 40 {
		if time.Now().After(deadline) {
			t.Fatal("streamed bytes never reached the progress API")
		}
		time.Sleep(time.Millisecond)
	}
	progress := m.currentImportProgress()
	if progress.Percent == nil || *progress.Percent != 40 || progress.Message != "downloading digest" {
		t.Fatalf("progress = %+v", progress)
	}
	close(continuePull)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOllamaPullFailsOnStreamErrorOrMissingSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, stream, want string
	}{
		{"registry error", "{\"error\":\"registry unavailable\"}\n", "registry unavailable"},
		{"missing success", "{\"status\":\"pulling manifest\"}\n", "without a success event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.stream)) }))
			defer backend.Close()
			m := testManager(t)
			m.backend, _ = url.Parse(backend.URL)
			m.beginImportProgress("model:1b", "model:1b", 0, "Pulling model")
			if err := m.pullOllamaModel(context.Background(), "model:1b", time.Second); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("pull error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOllamaPullTimesOutWithoutProgress(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"status\":\"pulling manifest\"}\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer backend.Close()
	m := testManager(t)
	m.backend, _ = url.Parse(backend.URL)
	m.beginImportProgress("model:1b", "model:1b", 0, "Pulling model")
	if err := m.pullOllamaModel(context.Background(), "model:1b", 30*time.Millisecond); err == nil || !strings.Contains(err.Error(), "no progress") {
		t.Fatalf("timeout error = %v", err)
	}
}
