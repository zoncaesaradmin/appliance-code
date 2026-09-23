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

const (
	catalogInterval       = 24 * time.Hour
	catalogRetryInterval  = 15 * time.Minute
	catalogRefreshTimeout = 2 * time.Minute
	catalogSchemaVersion  = 4 // v4 recalculates Ollama memory from cached model-layer weights.
)

type catalogEntry struct {
	ID                string            `json:"id"`
	Source            string            `json:"source"`
	DownloadBytes     uint64            `json:"downloadBytes"`
	MemoryBytes       uint64            `json:"memoryBytes"`
	RequiredBytes     uint64            `json:"requiredBytes"`
	ModelContextLimit uint64            `json:"modelContextLimit,omitempty"`
	KVBytesPerToken   uint64            `json:"kvBytesPerToken,omitempty"`
	LaunchArguments   []string          `json:"launchArguments,omitempty"`
	Eligible          bool              `json:"eligible"`
	Reason            string            `json:"reason,omitempty"`
	Capabilities      modelCapabilities `json:"capabilities"`
}

type catalogState struct {
	SchemaVersion        int            `json:"schemaVersion"`
	Engine               string         `json:"engine"`
	RuntimeVersion       string         `json:"runtimeVersion,omitempty"`
	LastAttempt          time.Time      `json:"lastAttempt"`
	LastSuccess          time.Time      `json:"lastSuccess"`
	LastError            string         `json:"lastError,omitempty"`
	Refreshing           bool           `json:"refreshing"`
	Stale                bool           `json:"stale"`
	AvailableMemoryBytes uint64         `json:"availableMemoryBytes"`
	Items                []catalogEntry `json:"items"`
	// Discovery is deliberately bounded, not an exhaustive upstream mirror.
	Scope string `json:"scope"`
	Sort  string `json:"sort,omitempty"`
	Order string `json:"order,omitempty"`
}

type modelCatalog struct {
	mu                    sync.Mutex
	state                 catalogState
	m                     *manager
	discover              func(context.Context) ([]catalogEntry, error)
	budget                func(context.Context) (uint64, uint64)
	path                  string
	initialRefreshPending bool
}

func newModelCatalog(m *manager) *modelCatalog {
	c := &modelCatalog{m: m, path: filepath.Join(m.modelsDir, ".appliance-catalog", m.engine+".json"), initialRefreshPending: true}
	runtimeVersion := m.catalogRuntimeVersion()
	c.state = catalogState{SchemaVersion: catalogSchemaVersion, Engine: m.engine, RuntimeVersion: runtimeVersion, Items: []catalogEntry{}, Scope: "Popular upstream models; conservative estimates, load verification required"}
	if b, err := os.ReadFile(c.path); err == nil {
		var saved catalogState
		if json.Unmarshal(b, &saved) == nil && (saved.SchemaVersion == catalogSchemaVersion || saved.SchemaVersion == 3 || saved.SchemaVersion == 2) && saved.Engine == m.engine && saved.RuntimeVersion == runtimeVersion {
			// The previous schema contains usable candidates and size estimates.
			// Preserve them across an offline upgrade, but do not present its
			// unverified Ollama capability labels as authoritative.
			if saved.SchemaVersion == 2 && m.engine == "ollama" {
				for i := range saved.Items {
					saved.Items[i].Capabilities = unknownCapabilities()
				}
			}
			if saved.SchemaVersion < 4 && m.engine == "ollama" {
				for i := range saved.Items {
					// Older catalogs stored weights*2+2Gi. Preserve offline
					// candidates while replacing that inflated estimate.
					if saved.Items[i].MemoryBytes >= 2<<30 {
						weights := (saved.Items[i].MemoryBytes - (2 << 30)) / 2
						saved.Items[i].MemoryBytes = ollamaMemoryEstimate(weights)
					}
				}
			}
			saved.SchemaVersion = catalogSchemaVersion
			c.state = saved
			c.state.Refreshing = false
		}
	}
	c.discover = m.discoverModels
	c.budget = m.catalogBudget
	return c
}

func (m *manager) catalogRuntimeVersion() string {
	if m == nil || m.engine != "ollama" {
		return ""
	}
	version, err := ollamaRuntimeVersion()
	if err != nil {
		return ""
	}
	return version
}

func (c *modelCatalog) nextRefreshDelay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Every process start attempts discovery in the background while the saved
	// cache remains readable. Thereafter success is daily and failures retry
	// after a bounded delay, never in a tight loop.
	if c.initialRefreshPending {
		return 0
	}
	if c.state.LastError != "" {
		return time.Until(c.state.LastAttempt.Add(catalogRetryInterval))
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
	c.initialRefreshPending = false
	c.state.LastAttempt = time.Now().UTC()
	c.mu.Unlock()
	refreshCtx, cancel := context.WithTimeout(ctx, catalogRefreshTimeout)
	defer cancel()
	items, err := c.discover(refreshCtx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Refreshing = false
	if err == nil && len(items) == 0 {
		err = errors.New("upstream discovery returned no compatible candidates")
	}
	if err != nil && len(items) > 0 {
		// A temporary failure for one Ollama family or manifest must not hide
		// every candidate. Retain prior entries for missing ids, publish fresh
		// results, and keep the catalog stale so it retries soon.
		merged := make(map[string]catalogEntry, len(c.state.Items)+len(items))
		for _, item := range c.state.Items {
			merged[item.ID] = item
		}
		for _, item := range items {
			if old, ok := merged[item.ID]; ok && item.Capabilities.Verification == "unverified" && old.Capabilities.Verification != "unverified" {
				item.Capabilities = old.Capabilities
			}
			merged[item.ID] = item
		}
		c.state.Items = make([]catalogEntry, 0, len(merged))
		for _, item := range merged {
			c.state.Items = append(c.state.Items, item)
		}
		sort.Slice(c.state.Items, func(i, j int) bool { return c.state.Items[i].ID < c.state.Items[j].ID })
		c.state.LastSuccess = time.Now().UTC()
		c.state.LastError = "Catalog partially refreshed; retry scheduled. " + err.Error()
		log.Printf("model catalog refresh engine=%s partially succeeded candidates=%d: %v", c.m.engine, len(c.state.Items), err)
	} else if err != nil {
		c.state.LastError = "Catalog refresh failed; retaining the previous catalog. " + err.Error()
		log.Printf("model catalog refresh engine=%s failed: %v", c.m.engine, err)
	} else {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		c.state.SchemaVersion = catalogSchemaVersion
		c.state.RuntimeVersion = c.m.catalogRuntimeVersion()
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
	state.AvailableMemoryBytes = memory
	engine := ""
	mode := ""
	if c.m != nil {
		engine = c.m.engine
		mode = c.m.deviceLabel(ctx)
		if engine == "vllm" {
			usingGPU, _ := c.m.resolveDevice(ctx)
			if !usingGPU {
				mode = ""
			}
		}
	}
	for i := range state.Items {
		item := &state.Items[i]
		item.Eligible = false
		var planErr error
		if engine == "vllm" {
			if mode == "" {
				continue
			}
			serve, err := planServe(serveWindowInput{
				Engine:             engine,
				Mode:               mode,
				ModelEstimateBytes: item.MemoryBytes,
				AvailableBytes:     memory,
				AvailableCPUs:      hostCPUCount(),
				ModelContextLimit:  item.ModelContextLimit,
				KVBytesPerToken:    item.KVBytesPerToken,
			})
			planErr = err
			item.RequiredBytes = serve.RequiredBytes
			if serve.MaxModelLen > 0 {
				item.LaunchArguments = launchArgsForCatalog(serve.MaxModelLen)
			} else if item.ModelContextLimit > 0 {
				item.LaunchArguments = launchArgsForCatalog(item.ModelContextLimit)
			}
		} else {
			plan, err := planModelMemory(engine, item.MemoryBytes, memory)
			planErr = err
			item.RequiredBytes = plan.RequiredBytes
		}
		switch {
		case memory == 0:
			item.Reason = "Usable runtime memory could not be confirmed"
		case item.MemoryBytes == 0 || planErr != nil:
			if planErr != nil {
				item.Reason = planErr.Error()
			} else {
				item.Reason = "Insufficient available inference memory"
			}
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
	sortBy, order, err := normalizeCatalogSort(r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := m.catalog.snapshot(r.Context())
	sortCatalogItems(state.Items, sortBy, order)
	state.Sort = sortBy
	state.Order = order
	writeJSON(w, http.StatusOK, state)
}
