package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ollamaInventory is a durable, read-optimized snapshot of the local Ollama
// model store. It deliberately lives beside the manager registry on the models
// PVC so an appliance restart does not make the AI Services page wait on the
// daemon, disk scan, or one /api/show request per model.
type ollamaInventory struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Models    []model   `json:"models"`
}

func (m *manager) ollamaInventoryPath() string {
	return filepath.Join(m.modelsDir, ".zon", "ollama-inventory.json")
}

func (m *manager) loadOllamaInventory() error {
	m.ollama = ollamaInventory{Models: []model{}}
	if m.engine != "ollama" {
		return nil
	}
	b, err := os.ReadFile(m.ollamaInventoryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &m.ollama); err != nil {
		return fmt.Errorf("decode inventory: %w", err)
	}
	if m.ollama.Models == nil {
		m.ollama.Models = []model{}
	}
	return nil
}

func (m *manager) saveOllamaInventoryLocked() error {
	dir := filepath.Dir(m.ollamaInventoryPath())
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.ollama, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "ollama-inventory-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o660); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.ollamaInventoryPath())
}

func (m *manager) ollamaInventorySnapshot() []model {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model, len(m.ollama.Models))
	copy(items, m.ollama.Models)
	for index := range items {
		items[index].Path = ""
	}
	return items
}

func (m *manager) refreshOllamaInventory(ctx context.Context) {
	if m.engine != "ollama" {
		return
	}
	if err := m.ensureOllamaDaemon(ctx); err != nil {
		log.Printf("Ollama inventory refresh: ensure daemon: %v", err)
		return
	}
	var response struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := m.callBackend(ctx, http.MethodGet, "/api/tags", nil, &response); err != nil {
		log.Printf("Ollama inventory refresh: list tags: %v", err)
		return
	}
	items := make([]model, 0, len(response.Models))
	for _, tag := range response.Models {
		var details struct {
			Capabilities []string `json:"capabilities"`
			Template     string   `json:"template"`
		}
		capabilities := chatCapabilities()
		if err := m.callBackend(ctx, http.MethodPost, "/api/show", map[string]any{"name": tag.Name}, &details); err != nil {
			log.Printf("Ollama inventory refresh: model=%q capability discovery: %v", tag.Name, err)
		} else {
			capabilities = ollamaCapabilities(details.Capabilities, details.Template)
		}
		items = append(items, model{ID: tag.Name, Object: "model", OwnedBy: "ollama", OpenAIOwnedBy: "ollama", Capabilities: capabilities})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	m.mu.Lock()
	m.ollama = ollamaInventory{UpdatedAt: time.Now().UTC(), Models: items}
	if err := m.saveOllamaInventoryLocked(); err != nil {
		log.Printf("Ollama inventory refresh: persist: %v", err)
	}
	m.mu.Unlock()
}
