package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileInterruptedImportProgressMarksFailed(t *testing.T) {
	m := testManager(t)
	if err := os.MkdirAll(filepath.Join(m.modelsDir, ".downloads", "orphan-tmp"), 0o770); err != nil {
		t.Fatal(err)
	}
	saved := importProgress{
		ModelID: "org/model",
		Source:  "org/model@abc",
		State:   "downloading",
		Message: "Downloading model files",
	}
	payload, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.progressPath(), payload, 0o640); err != nil {
		t.Fatal(err)
	}

	m.reconcileInterruptedProgress()

	got := m.currentImportProgress()
	if got.State != "failed" || got.ModelID != "org/model" {
		t.Fatalf("progress=%+v", got)
	}
	if _, err := os.Stat(filepath.Join(m.modelsDir, ".downloads", "orphan-tmp")); !os.IsNotExist(err) {
		t.Fatalf("orphan download dir still present: %v", err)
	}
}

func TestRehydrateActiveModelWhenBackendHealthy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	m := testManager(t)
	m.backend, _ = url.Parse(backend.URL)
	m.finishLoadProgress("ready", "org/model", "Model is ready for use")
	m.rehydrateActiveModel(context.Background())
	m.processMu.Lock()
	active := m.active
	m.processMu.Unlock()
	if active != "org/model" {
		t.Fatalf("active=%q", active)
	}
	state, id := m.servingSnapshot()
	if state != "ready" || id != "org/model" {
		t.Fatalf("serving=%s id=%s", state, id)
	}
}
