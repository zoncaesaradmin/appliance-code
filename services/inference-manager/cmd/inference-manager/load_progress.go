package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// loadProgress is the operator- and UI-visible model-load status.
// Persisted under modelsDir/.zon/load-progress.json for host inspection.
type loadProgress struct {
	ModelID   string    `json:"modelId,omitempty"`
	State     string    `json:"state"` // idle|loading|ready|failed
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (m *manager) loadProgressPath() string {
	return filepath.Join(m.modelsDir, ".zon", "load-progress.json")
}

func (m *manager) loadProgressHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, m.currentLoadProgress())
}

func (m *manager) currentLoadProgress() loadProgress {
	m.loadMu.RLock()
	defer m.loadMu.RUnlock()
	if m.load.State == "" {
		return loadProgress{State: "idle", UpdatedAt: time.Now().UTC()}
	}
	return m.load
}

func (m *manager) beginLoadProgress(modelID, message string) {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()
	m.load = loadProgress{
		ModelID:   modelID,
		State:     "loading",
		Message:   message,
		UpdatedAt: time.Now().UTC(),
	}
	_ = m.writeLoadProgressFileLocked()
}

func (m *manager) finishLoadProgress(state, modelID, message string) {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()
	m.load = loadProgress{
		ModelID:   modelID,
		State:     state,
		UpdatedAt: time.Now().UTC(),
	}
	if state == "failed" {
		m.load.Error = message
		m.load.Message = "Load failed"
	} else {
		m.load.Message = message
	}
	_ = m.writeLoadProgressFileLocked()
}

func (m *manager) writeLoadProgressFileLocked() error {
	path := m.loadProgressPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(m.load, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *manager) servingSnapshot() (servingState, loadedModelID string) {
	load := m.currentLoadProgress()
	m.processMu.Lock()
	active := m.active
	m.processMu.Unlock()
	switch load.State {
	case "loading":
		return "loading", load.ModelID
	case "failed":
		return "failed", load.ModelID
	case "ready":
		if active != "" {
			return "ready", active
		}
		return "ready", load.ModelID
	default:
		if active != "" {
			return "ready", active
		}
		return "inactive", ""
	}
}
