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
	"sync"
	"time"
)

const catalogInterval = 24 * time.Hour

type catalogEntry struct {
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	DownloadBytes   uint64   `json:"downloadBytes"`
	MemoryBytes     uint64   `json:"memoryBytes"`
	LaunchArguments []string `json:"launchArguments,omitempty"`
	Eligible        bool     `json:"eligible"`
	Reason          string   `json:"reason,omitempty"`
}

type catalogState struct {
	Engine      string         `json:"engine"`
	LastAttempt time.Time      `json:"lastAttempt"`
	LastSuccess time.Time      `json:"lastSuccess"`
	LastError   string         `json:"lastError,omitempty"`
	Refreshing  bool           `json:"refreshing"`
	Stale       bool           `json:"stale"`
	Items       []catalogEntry `json:"items"`
	// Discovery is deliberately bounded, not an exhaustive upstream mirror.
	Scope string `json:"scope"`
}

type modelCatalog struct {
	mu       sync.Mutex
	state    catalogState
	m        *manager
	discover func(context.Context) ([]catalogEntry, error)
	budget   func(context.Context) (uint64, uint64)
	path     string
}

func newModelCatalog(m *manager) *modelCatalog {
	c := &modelCatalog{m: m, path: filepath.Join(m.modelsDir, ".appliance-catalog", m.engine+".json")}
	c.state = catalogState{Engine: m.engine, Items: []catalogEntry{}, Scope: "Popular upstream models; conservative estimates, load verification required"}
	if b, err := os.ReadFile(c.path); err == nil {
		var saved catalogState
		if json.Unmarshal(b, &saved) == nil && saved.Engine == m.engine {
			c.state = saved
			c.state.Refreshing = false
		}
	}
	c.discover = m.discoverModels
	c.budget = m.catalogBudget
	return c
}

func (c *modelCatalog) nextRefreshDelay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Successful catalogs (or retained good ones) keep the daily schedule so
	// restarts do not hammer upstream. A never-successful / empty failed
	// catalog must retry on the next start; otherwise a one-time probe bug
	// hides behind a 24h lock after the fix is deployed.
	if !c.state.LastAttempt.IsZero() && (c.state.LastSuccess.IsZero() || len(c.state.Items) == 0) && c.state.LastError != "" {
		return 0
	}
	return time.Until(c.state.LastAttempt.Add(catalogInterval))
}

func (c *modelCatalog) run(ctx context.Context) {
	for {
		delay := c.nextRefreshDelay()
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		c.refresh(ctx)
	}
}

func (c *modelCatalog) refresh(ctx context.Context) {
	c.mu.Lock()
	if c.state.Refreshing {
		c.mu.Unlock()
		return
	}
	c.state.Refreshing = true
	c.state.LastAttempt = time.Now().UTC()
	c.mu.Unlock()
	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	items, err := c.discover(refreshCtx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Refreshing = false
	if err == nil && len(items) == 0 {
		err = errors.New("upstream discovery returned no compatible candidates")
	}
	if err != nil {
		c.state.LastError = "Catalog refresh failed; retaining the previous catalog. " + err.Error()
		log.Printf("model catalog refresh engine=%s failed: %v", c.m.engine, err)
	} else {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		c.state.Items, c.state.LastSuccess, c.state.LastError = items, time.Now().UTC(), ""
		log.Printf("model catalog refreshed engine=%s candidates=%d", c.m.engine, len(items))
	}
	if err := c.save(); err != nil {
		c.state.LastError = "Catalog persistence failed; results may be lost on restart"
		log.Printf("model catalog persistence: %v", err)
	}
}

func (c *modelCatalog) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o770); err != nil {
		return err
	}
	b, err := json.Marshal(c.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), "catalog-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.path)
}

func (c *modelCatalog) snapshot(ctx context.Context) catalogState {
	c.mu.Lock()
	state := c.state
	state.Items = append([]catalogEntry{}, c.state.Items...)
	c.mu.Unlock()
	state.Stale = state.LastSuccess.IsZero() || time.Since(state.LastSuccess) >= catalogInterval || state.LastError != ""
	memory, disk := c.budget(ctx)
	for i := range state.Items {
		item := &state.Items[i]
		item.Eligible = false
		switch {
		case memory == 0:
			item.Reason = "Usable runtime memory could not be confirmed"
		case item.MemoryBytes == 0 || item.MemoryBytes > memory:
			item.Reason = "Insufficient available inference memory"
		case disk == 0 || item.DownloadBytes == 0 || item.DownloadBytes > disk/2:
			item.Reason = "Insufficient model storage including download workspace"
		default:
			item.Eligible = true
			item.Reason = "Estimated fit; verified when loaded"
		}
	}
	return state
}

func (c *modelCatalog) selection(ctx context.Context, id string) (catalogEntry, error) {
	for _, item := range c.snapshot(ctx).Items {
		if item.ID == id {
			if !item.Eligible {
				return catalogEntry{}, fmt.Errorf("model is not currently eligible: %s", item.Reason)
			}
			return item, nil
		}
	}
	return catalogEntry{}, errors.New("model is not in the cached catalog")
}

func (m *manager) modelCatalog(w http.ResponseWriter, r *http.Request) {
	if m.catalog == nil {
		writeError(w, http.StatusServiceUnavailable, "catalog is initializing")
		return
	}
	writeJSON(w, http.StatusOK, m.catalog.snapshot(r.Context()))
}
