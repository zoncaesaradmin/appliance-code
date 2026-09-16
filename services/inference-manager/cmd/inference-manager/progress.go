package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// importProgress is the operator- and UI-visible download status.
// Persisted under modelsDir/.downloads/.progress.json for host inspection.
type importProgress struct {
	ModelID         string    `json:"modelId,omitempty"`
	Source          string    `json:"source,omitempty"`
	State           string    `json:"state"` // idle|downloading|verifying|installing|complete|failed
	BytesDownloaded uint64    `json:"bytesDownloaded,omitempty"`
	BytesTotal      uint64    `json:"bytesTotal,omitempty"`
	Percent         *int      `json:"percent,omitempty"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (m *manager) progressPath() string {
	return filepath.Join(m.modelsDir, ".downloads", ".progress.json")
}

func (m *manager) importProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, m.currentImportProgress())
}

func (m *manager) currentImportProgress() importProgress {
	m.progressMu.RLock()
	defer m.progressMu.RUnlock()
	if m.progress.State == "" {
		return importProgress{State: "idle", UpdatedAt: time.Now().UTC()}
	}
	out := m.progress
	return out
}

func (m *manager) beginImportProgress(modelID, source string, bytesTotal uint64, message string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	m.progress = importProgress{
		ModelID:    modelID,
		Source:     source,
		State:      "downloading",
		BytesTotal: bytesTotal,
		Message:    message,
		UpdatedAt:  time.Now().UTC(),
	}
	if bytesTotal > 0 {
		percent := 0
		m.progress.Percent = &percent
	}
	_ = m.writeProgressFileLocked()
}

func (m *manager) setImportProgressState(state, message string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if m.progress.State == "" || m.progress.State == "idle" {
		return
	}
	m.progress.State = state
	if message != "" {
		m.progress.Message = message
	}
	m.progress.UpdatedAt = time.Now().UTC()
	_ = m.writeProgressFileLocked()
}

func (m *manager) updateDownloadBytes(downloaded, bytesTotal uint64) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if m.progress.State != "downloading" {
		return
	}
	if downloaded > m.progress.BytesDownloaded {
		m.progress.BytesDownloaded = downloaded
	}
	if bytesTotal > 0 {
		m.progress.BytesTotal = bytesTotal
	}
	if m.progress.BytesTotal > 0 {
		percent := int(m.progress.BytesDownloaded * 100 / m.progress.BytesTotal)
		if percent > 100 {
			percent = 100
		}
		m.progress.Percent = &percent
	}
	m.progress.UpdatedAt = time.Now().UTC()
	_ = m.writeProgressFileLocked()
}

func (m *manager) finishImportProgress(state, message string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if m.progress.ModelID == "" && m.progress.State == "" {
		m.progress = importProgress{State: "idle", UpdatedAt: time.Now().UTC()}
		_ = m.writeProgressFileLocked()
		return
	}
	m.progress.State = state
	m.progress.UpdatedAt = time.Now().UTC()
	if state == "failed" {
		m.progress.Error = message
		m.progress.Message = "Download failed"
	} else {
		m.progress.Error = ""
		m.progress.Message = message
	}
	if state == "complete" && m.progress.BytesTotal > 0 {
		m.progress.BytesDownloaded = m.progress.BytesTotal
		percent := 100
		m.progress.Percent = &percent
	}
	_ = m.writeProgressFileLocked()
}

func (m *manager) writeProgressFileLocked() error {
	path := m.progressPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(m.progress, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// reconcileInterruptedProgress marks in-flight download/load state as failed
// after a manager restart (OOMKill, pod recycle). Without this the progress
// file says "downloading" while the API reports idle and new imports conflict.
func (m *manager) reconcileInterruptedProgress() {
	m.reconcileInterruptedImportProgress()
	m.reconcileInterruptedLoadProgress()
	m.cleanupOrphanDownloads()
}

func (m *manager) reconcileInterruptedImportProgress() {
	path := m.progressPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved importProgress
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	switch saved.State {
	case "downloading", "verifying", "installing":
	default:
		m.progressMu.Lock()
		m.progress = saved
		m.progressMu.Unlock()
		return
	}
	m.progressMu.Lock()
	m.progress = saved
	m.progress.State = "failed"
	m.progress.Error = "Download interrupted when the inference manager restarted; retry the download"
	m.progress.Message = "Download failed"
	m.progress.UpdatedAt = time.Now().UTC()
	_ = m.writeProgressFileLocked()
	m.progressMu.Unlock()
	log.Printf("import progress recovered as failed after restart model=%s", saved.ModelID)
}

func (m *manager) cleanupOrphanDownloads() {
	root := filepath.Join(m.modelsDir, ".downloads")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}

// watchDownloadDir polls destination size while a Hugging Face snapshot download runs.
func (m *manager) watchDownloadDir(dir string, bytesTotal uint64) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				size, err := directorySize(dir)
				if err != nil {
					continue
				}
				m.updateDownloadBytes(size, bytesTotal)
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func directorySize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	return total, err
}
