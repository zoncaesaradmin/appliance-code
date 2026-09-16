package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var modelRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@:-]{0,511}$`)

type model struct {
	ID              string    `json:"id"`
	Object          string    `json:"object"`
	OwnedBy         string    `json:"ownedBy"`
	OpenAIOwnedBy   string    `json:"owned_by"`
	Source          string    `json:"source"`
	Digest          string    `json:"digest"`
	Path            string    `json:"path"`
	InstalledAt     time.Time `json:"installedAt"`
	LaunchArguments []string  `json:"launchArguments,omitempty"`
}

type registry struct {
	Models map[string]model `json:"models"`
}

type manager struct {
	engine      string
	modelsDir   string
	controlDir  string
	backend     *url.URL
	proxy       *httputil.ReverseProxy
	mu          sync.RWMutex // protects reg only
	opMu        sync.Mutex   // single-flight import/load/delete (held for async import/load lifetime)
	progressMu  sync.RWMutex
	progress    importProgress
	loadMu      sync.RWMutex
	load        loadProgress
	processMu   sync.Mutex
	process     *exec.Cmd
	processDone chan struct{}
	active      string
	generation  string
	reg         registry
	download    func(context.Context, string, string) error
	start       func(context.Context, model) error
	cudaProbe   func(context.Context) bool
	catalog     *modelCatalog
}

type importRequest struct {
	ModelID         string
	Source          string
	Digest          string
	LaunchArguments []string
	CatalogID       string
	BytesTotal      uint64
}

func main() {
	engine := strings.ToLower(env("INFERENCE_ENGINE", "vllm"))
	modelsDir := env("INFERENCE_MODELS_DIR", "/models")
	controlDir := env("INFERENCE_CONTROL_DIR", "/control")
	backend, _ := url.Parse(env("INFERENCE_BACKEND_URL", "http://127.0.0.1:8001"))
	m := &manager{engine: engine, modelsDir: modelsDir, controlDir: controlDir, backend: backend, proxy: httputil.NewSingleHostReverseProxy(backend)}
	m.download = m.downloadModel
	if engine == "ollama" {
		m.start = m.startOllamaModel
	} else {
		m.start = m.startVLLM
	}
	m.cudaProbe = cudaAvailable
	if err := m.loadRegistry(); err != nil {
		log.Fatalf("load model registry: %v", err)
	}
	m.reconcileInterruptedProgress()
	m.rehydrateActiveModel(context.Background())
	if engine == "ollama" {
		if err := m.waitEngineReady(context.Background()); err != nil {
			log.Fatalf("wait for Ollama engine sidecar: %v", err)
		}
	}

	server := &http.Server{Addr: env("INFERENCE_LISTEN_ADDRESS", "0.0.0.0:11434"), Handler: m.handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	m.catalog = newModelCatalog(m)
	go m.catalog.run(ctx)
	go func() {
		<-ctx.Done()
		m.stopProcess()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("inference manager listening on %s (engine=%s)", server.Addr, engine)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (m *manager) handler() http.Handler {
	mux := http.NewServeMux()
	// Match only the root: a GET subtree conflicts with the all-method proxy.
	mux.HandleFunc("GET /{$}", m.health)
	mux.HandleFunc("GET /internal/v1/runtime/capabilities", m.capabilities)
	mux.HandleFunc("POST /internal/v1/models/imports", m.importModel)
	mux.HandleFunc("GET /internal/v1/models/imports/progress", m.importProgress)
	// Body-based actions: Hugging Face ids contain "/", which breaks single-segment path params.
	mux.HandleFunc("POST /internal/v1/models/load", m.loadModel)
	mux.HandleFunc("GET /internal/v1/models/load/progress", m.loadProgressHandler)
	mux.HandleFunc("POST /internal/v1/models/delete", m.deleteModel)
	mux.HandleFunc("GET /v1/models", m.listModels)
	mux.HandleFunc("GET /internal/v1/models/catalog", m.modelCatalog)
	mux.HandleFunc("/v1/", m.proxyOpenAI)
	return mux
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (m *manager) registryPath() string { return filepath.Join(m.modelsDir, ".zon", "models.json") }

func (m *manager) loadRegistry() error {
	m.reg.Models = map[string]model{}
	b, err := os.ReadFile(m.registryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &m.reg)
}

func (m *manager) saveRegistry() error {
	dir := filepath.Dir(m.registryPath())
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.reg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "models-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o660); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.registryPath())
}

func (m *manager) health(w http.ResponseWriter, _ *http.Request) {
	servingState, loadedModelID := m.servingSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"engine":        m.engine,
		"loadedModelId": loadedModelID,
		"servingState":  servingState,
	})
}

func (m *manager) capabilities(w http.ResponseWriter, _ *http.Request) {
	available, mode, checks := m.selectMode(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"availableModes": available, "activeMode": mode, "checks": checks})
}

// selectMode is package- and runtime-driven. Auto prefers a verified CUDA
// backend, then CPU. It never derives GPU support from an architecture, vendor,
// or machine name.
func (m *manager) selectMode(ctx context.Context) ([]string, string, []map[string]string) {
	supported := map[string]bool{}
	for _, mode := range strings.Split(env("INFERENCE_SUPPORTED_MODES", "cpu"), ",") {
		supported[strings.ToLower(strings.TrimSpace(mode))] = true
	}
	available := make([]string, 0, 2)
	checks := make([]map[string]string, 0, 2)
	if supported["cuda"] {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cuda := m.cudaProbe(probeCtx)
		cancel()
		if cuda {
			available = append(available, "cuda")
			checks = append(checks, map[string]string{"name": "cuda-runtime", "status": "pass", "message": "CUDA backend and visible device confirmed"})
		} else {
			checks = append(checks, map[string]string{"name": "cuda-runtime", "status": "fail", "message": "CUDA backend or visible device is unavailable"})
		}
	}
	if supported["cpu"] {
		if runtime.GOARCH != "amd64" || cpuHasFeature("avx2") {
			available = append(available, "cpu")
			checks = append(checks, map[string]string{"name": "cpu-runtime", "status": "pass", "message": runtime.GOARCH})
		} else {
			checks = append(checks, map[string]string{"name": "cpu-runtime", "status": "fail", "message": "x86 CPU does not report AVX2"})
		}
	}
	requested := strings.ToLower(env("INFERENCE_MODE", "auto"))
	if requested == "auto" {
		for _, preferred := range []string{"cuda", "cpu"} {
			for _, mode := range available {
				if mode == preferred {
					return available, mode, checks
				}
			}
		}
		return available, "", checks
	}
	for _, mode := range available {
		if mode == requested {
			return available, mode, checks
		}
	}
	checks = append(checks, map[string]string{"name": "requested-mode", "status": "fail", "message": "requested mode is not usable"})
	return available, "", checks
}

func cudaAvailable(ctx context.Context) bool {
	// The manager no longer embeds torch. Prefer an explicit install-time GPU
	// enablement signal, then fall back to a visible NVIDIA device node.
	switch strings.ToLower(strings.TrimSpace(env("INFERENCE_GPU_ENABLED", ""))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	if visible := strings.TrimSpace(env("NVIDIA_VISIBLE_DEVICES", "")); visible == "" || visible == "void" || visible == "none" {
		return false
	}
	if _, err := os.Stat("/dev/nvidia0"); err == nil {
		return true
	}
	cmd := exec.CommandContext(ctx, "nvidia-smi")
	return cmd.Run() == nil
}

func cpuHasFeature(feature string) bool {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.ToLower(string(b)), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || (strings.TrimSpace(parts[0]) != "flags" && strings.TrimSpace(parts[0]) != "features") {
			continue
		}
		for _, flag := range strings.Fields(parts[1]) {
			if flag == feature {
				return true
			}
		}
	}
	return false
}

func (m *manager) listModels(w http.ResponseWriter, _ *http.Request) {
	if m.engine == "ollama" {
		m.listOllamaModels(w)
		return
	}
	// Registry read lock only — must not wait on import/download.
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]model, 0, len(m.reg.Models))
	for _, item := range m.reg.Models {
		item.Path = ""
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
}

func (m *manager) importModel(w http.ResponseWriter, r *http.Request) {
	if !m.opMu.TryLock() {
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	var req struct {
		ModelID         string
		Source          string
		Digest          string
		LaunchArguments []string `json:"launchArguments"`
		CatalogID       string   `json:"catalogId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		m.opMu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !modelRefRE.MatchString(req.ModelID) || !modelRefRE.MatchString(req.Source) {
		m.opMu.Unlock()
		writeError(w, http.StatusBadRequest, "modelId and source must be repository model references")
		return
	}
	job := importRequest{ModelID: req.ModelID, Source: req.Source, Digest: req.Digest, LaunchArguments: append([]string(nil), req.LaunchArguments...), CatalogID: req.CatalogID}
	if req.CatalogID != "" {
		if m.catalog == nil {
			m.opMu.Unlock()
			writeError(w, http.StatusServiceUnavailable, "model catalog is not ready")
			return
		}
		entry, err := m.catalog.selection(r.Context(), req.CatalogID)
		if err != nil {
			m.opMu.Unlock()
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if req.ModelID != entry.ID || req.Source != entry.Source {
			m.opMu.Unlock()
			writeError(w, http.StatusConflict, "catalog selection changed; refresh the model list")
			return
		}
		job.LaunchArguments = append([]string(nil), entry.LaunchArguments...)
		job.BytesTotal = entry.DownloadBytes
	}
	m.mu.RLock()
	_, exists := m.reg.Models[req.ModelID]
	m.mu.RUnlock()
	if exists {
		m.opMu.Unlock()
		writeError(w, http.StatusConflict, "model is already installed")
		return
	}
	if m.engine != "ollama" {
		if err := validateVLLMArguments(job.LaunchArguments); err != nil {
			m.opMu.Unlock()
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if job.Digest != "" {
		m.opMu.Unlock()
		writeError(w, http.StatusUnprocessableEntity, "expected aggregate digest is not supported for Ollama pulls")
		return
	} else if job.ModelID != job.Source {
		m.opMu.Unlock()
		writeError(w, http.StatusBadRequest, "Ollama modelId must equal source")
		return
	}
	message := "Downloading model files"
	if m.engine == "ollama" {
		message = "Pulling model from the Ollama registry"
	}
	m.beginImportProgress(job.ModelID, job.Source, job.BytesTotal, message)
	// Request context ends when this handler returns; the job must outlive it.
	go m.runImport(context.Background(), job)
	writeJSON(w, http.StatusAccepted, m.currentImportProgress())
}

func (m *manager) runImport(ctx context.Context, req importRequest) {
	defer m.opMu.Unlock()
	if m.engine == "ollama" {
		m.runOllamaImport(ctx, req)
		return
	}
	m.runVLLMImport(ctx, req)
}

func (m *manager) runVLLMImport(ctx context.Context, req importRequest) {
	keySum := sha256.Sum256([]byte(req.ModelID))
	key := hex.EncodeToString(keySum[:16])
	downloadRoot := filepath.Join(m.modelsDir, ".downloads")
	if err := os.MkdirAll(downloadRoot, 0o770); err != nil {
		m.finishImportProgress("failed", err.Error())
		return
	}
	tmp, err := os.MkdirTemp(downloadRoot, key+"-")
	if err != nil {
		m.finishImportProgress("failed", err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	stopWatch := m.watchDownloadDir(tmp, req.BytesTotal)
	defer stopWatch()
	if err := m.download(ctx, req.Source, tmp); err != nil {
		m.finishImportProgress("failed", "model download failed: "+err.Error())
		return
	}
	stopWatch()
	m.setImportProgressState("verifying", "Verifying downloaded model")
	digest, err := directoryDigest(tmp)
	if err != nil {
		m.finishImportProgress("failed", "model verification failed: "+err.Error())
		return
	}
	if req.Digest != "" && req.Digest != digest {
		m.finishImportProgress("failed", fmt.Sprintf("model digest mismatch: got %s", digest))
		return
	}
	destination := filepath.Join(m.modelsDir, key)
	m.setImportProgressState("installing", "Installing model")
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.reg.Models[req.ModelID]; exists {
		m.finishImportProgress("failed", "model is already installed")
		return
	}
	if err := os.Rename(tmp, destination); err != nil {
		m.finishImportProgress("failed", "install model: "+err.Error())
		return
	}
	item := model{ID: req.ModelID, Object: "model", OwnedBy: "appliance", OpenAIOwnedBy: "appliance", Source: req.Source, Digest: digest, Path: destination, InstalledAt: time.Now().UTC(), LaunchArguments: append([]string(nil), req.LaunchArguments...)}
	m.reg.Models[item.ID] = item
	if err := m.saveRegistry(); err != nil {
		delete(m.reg.Models, item.ID)
		_ = os.RemoveAll(destination)
		m.finishImportProgress("failed", "save model registry: "+err.Error())
		return
	}
	m.finishImportProgress("complete", "Model downloaded")
}

func readModelID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		ModelID string `json:"modelId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	id := strings.TrimSpace(req.ModelID)
	if id == "" || !modelRefRE.MatchString(id) {
		writeError(w, http.StatusBadRequest, "modelId must be a repository model reference")
		return "", false
	}
	return id, true
}

func (m *manager) loadModel(w http.ResponseWriter, r *http.Request) {
	id, ok := readModelID(w, r)
	if !ok {
		return
	}
	if m.isModelReady(r.Context(), id) {
		m.finishLoadProgress("ready", id, "Model is ready for use")
		writeJSON(w, http.StatusAccepted, m.currentLoadProgress())
		return
	}
	if !m.opMu.TryLock() {
		progress := m.currentLoadProgress()
		if progress.ModelID == id && progress.State == "loading" {
			writeJSON(w, http.StatusAccepted, progress)
			return
		}
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	if m.engine != "ollama" {
		m.mu.RLock()
		_, exists := m.reg.Models[id]
		m.mu.RUnlock()
		if !exists {
			m.opMu.Unlock()
			writeError(w, http.StatusNotFound, "model is not installed")
			return
		}
	}
	m.beginLoadProgress(id, "Loading model into the inference engine")
	go m.runLoad(context.Background(), id)
	writeJSON(w, http.StatusAccepted, m.currentLoadProgress())
}

func (m *manager) runLoad(ctx context.Context, id string) {
	defer m.opMu.Unlock()
	if m.engine == "ollama" {
		if err := m.callBackend(ctx, http.MethodPost, "/api/generate", map[string]any{"model": id, "prompt": "", "stream": false, "keep_alive": "5m"}, nil); err != nil {
			m.finishLoadProgress("failed", id, err.Error())
			return
		}
		m.processMu.Lock()
		m.active = id
		m.processMu.Unlock()
		m.finishLoadProgress("ready", id, "Model is ready for use")
		return
	}
	m.mu.RLock()
	item, ok := m.reg.Models[id]
	m.mu.RUnlock()
	if !ok {
		m.finishLoadProgress("failed", id, "model is not installed")
		return
	}
	m.stopProcess()
	if err := m.start(ctx, item); err != nil {
		m.finishLoadProgress("failed", id, "start vLLM: "+err.Error())
		return
	}
	m.processMu.Lock()
	m.active = id
	m.processMu.Unlock()
	if err := m.waitVLLMReady(ctx); err != nil {
		m.stopProcess()
		m.finishLoadProgress("failed", id, "vLLM did not become ready: "+err.Error())
		return
	}
	m.finishLoadProgress("ready", id, "Model is ready for use")
}

func (m *manager) isModelReady(ctx context.Context, id string) bool {
	m.processMu.Lock()
	active := m.active == id
	m.processMu.Unlock()
	if !active {
		return false
	}
	return m.backendHealthy(ctx)
}

func (m *manager) backendHealthy(ctx context.Context) bool {
	healthPath := "/health"
	if m.engine == "ollama" {
		healthPath = "/"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, m.backend.ResolveReference(&url.URL{Path: healthPath}).String(), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (m *manager) waitBackend(ctx context.Context, done <-chan struct{}) error {
	return m.waitBackendPath(ctx, done, "/health")
}

func (m *manager) waitVLLMReady(ctx context.Context) error {
	done := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, exited := m.engineExitedForCurrentGeneration(); exited {
				close(done)
				return
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	if err := m.waitBackendPath(ctx, done, "/health"); err != nil {
		if status, exited := m.engineExitedForCurrentGeneration(); exited {
			return fmt.Errorf("runtime process exited (code %d)", status.ExitCode)
		}
		if errors.Is(err, errRuntimeExited) {
			return err
		}
		return err
	}
	return nil
}

var errRuntimeExited = errors.New("runtime process exited")

func (m *manager) waitBackendPath(ctx context.Context, done <-chan struct{}, healthPath string) error {
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.backend.ResolveReference(&url.URL{Path: healthPath}).String(), nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			if done != nil {
				return errRuntimeExited
			}
		case <-deadline.C:
			return errors.New("startup timed out")
		case <-ticker.C:
		}
	}
}

func (m *manager) waitEngineReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	healthPath := "/health"
	if m.engine == "ollama" {
		healthPath = "/"
	}
	return m.waitBackendPath(ctx, nil, healthPath)
}

func (m *manager) deleteModel(w http.ResponseWriter, r *http.Request) {
	if !m.opMu.TryLock() {
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	defer m.opMu.Unlock()
	id, ok := readModelID(w, r)
	if !ok {
		return
	}
	if m.engine == "ollama" {
		if err := m.callBackend(r.Context(), http.MethodDelete, "/api/delete", map[string]any{"model": id}, nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.reg.Models[id]
	if !ok {
		writeError(w, http.StatusNotFound, "model is not installed")
		return
	}
	m.processMu.Lock()
	active := m.active == id
	m.processMu.Unlock()
	if active {
		m.stopProcess()
	}
	if err := os.RemoveAll(item.Path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	delete(m.reg.Models, id)
	if err := m.saveRegistry(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *manager) proxyOpenAI(w http.ResponseWriter, r *http.Request) {
	m.processMu.Lock()
	active := m.active
	m.processMu.Unlock()
	if active == "" && m.engine != "ollama" {
		writeError(w, http.StatusServiceUnavailable, "no model is loaded")
		return
	}
	m.proxy.ServeHTTP(w, r)
}

func (m *manager) startOllamaModel(context.Context, model) error {
	return errors.New("Ollama models are loaded through the running Ollama API")
}

func (m *manager) runOllamaImport(ctx context.Context, req importRequest) {
	if err := m.callBackend(ctx, http.MethodPost, "/api/pull", map[string]any{"model": req.Source, "stream": false}, nil); err != nil {
		m.finishImportProgress("failed", err.Error())
		return
	}
	m.finishImportProgress("complete", "Model downloaded")
}

func (m *manager) listOllamaModels(w http.ResponseWriter) {
	var response struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := m.callBackend(context.Background(), http.MethodGet, "/api/tags", nil, &response); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(response.Models))
	for _, item := range response.Models {
		items = append(items, map[string]any{"id": item.Name, "object": "model", "ownedBy": "ollama", "owned_by": "ollama"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
}

func (m *manager) callBackend(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(data))
	}
	requestURL := m.backend.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("engine returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	if target != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(target)
	}
	return nil
}

var vllmValueArguments = map[string]func(string) bool{
	"--quantization": func(value string) bool { return value != "" && !strings.HasPrefix(value, "-") },
	"--max-model-len": func(value string) bool {
		number, err := strconv.ParseUint(value, 10, 64)
		return err == nil && number > 0
	},
	"--gpu-memory-utilization": func(value string) bool {
		number, err := strconv.ParseFloat(value, 64)
		return err == nil && number > 0 && number <= 1
	},
	"--cudagraph-capture-sizes": func(value string) bool {
		number, err := strconv.ParseUint(value, 10, 64)
		return err == nil && number > 0
	},
	"--served-model-name": func(value string) bool { return modelRefRE.MatchString(value) },
	"--tool-call-parser":  func(value string) bool { return value != "" && !strings.HasPrefix(value, "-") },
}

var vllmBooleanArguments = map[string]bool{
	"--no-enable-flashinfer-autotune": true,
	"--enable-auto-tool-choice":       true,
}

func validateVLLMArguments(arguments []string) error {
	if len(arguments) > 32 {
		return errors.New("vLLM launch arguments may contain at most 32 entries")
	}
	seen := map[string]bool{}
	for index := 0; index < len(arguments); index++ {
		argument := strings.TrimSpace(arguments[index])
		if validator, ok := vllmValueArguments[argument]; ok {
			if seen[argument] || index+1 >= len(arguments) || !validator(strings.TrimSpace(arguments[index+1])) {
				return fmt.Errorf("invalid or duplicate vLLM argument %s", argument)
			}
			seen[argument] = true
			index++
			continue
		}
		if vllmBooleanArguments[argument] && !seen[argument] {
			seen[argument] = true
			continue
		}
		return fmt.Errorf("unsupported vLLM launch argument %q", argument)
	}
	return nil
}

func (m *manager) downloadModel(ctx context.Context, source, destination string) error {
	cmd := exec.CommandContext(ctx, env("INFERENCE_PYTHON", "python3"), "/usr/local/libexec/inference-manager/download_model.py", "--source", source, "--destination", destination)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	// Keep HF metadata/cache on the models volume, not the tiny emptyDir home.
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(m.modelsDir, ".cache", "huggingface"),
		"HUGGINGFACE_HUB_CACHE="+filepath.Join(m.modelsDir, ".cache", "huggingface", "hub"),
	)
	return cmd.Run()
}

func (m *manager) startVLLM(_ context.Context, item model) error {
	_, mode, _ := m.selectMode(context.Background())
	if mode == "" {
		return errors.New("no usable inference mode")
	}
	if err := os.MkdirAll(m.controlDir, 0o750); err != nil {
		return err
	}
	args := m.vllmCommandArguments(item, mode)
	command := append([]string{env("INFERENCE_VLLM_COMMAND", "vllm")}, args...)
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	argvPath := filepath.Join(m.controlDir, "argv.json")
	generation := fmt.Sprintf("%d", time.Now().UnixNano())
	m.clearEngineExitStatus()
	if err := os.WriteFile(argvPath, payload, 0o640); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.controlDir, "generation"), []byte(generation), 0o640); err != nil {
		return err
	}
	m.processMu.Lock()
	m.generation = generation
	m.process, m.processDone = nil, nil
	m.processMu.Unlock()
	_ = mode
	return nil
}

func (m *manager) vllmCommandArguments(item model, mode string) []string {
	args := []string{"serve", item.Path}
	args = append(args, clampLaunchMaxModelLen(item.LaunchArguments, item.Path)...)
	if !argumentPresent(item.LaunchArguments, "--served-model-name") && !argumentPresent(args, "--served-model-name") {
		args = append(args, "--served-model-name", item.ID)
	}
	// Device is fixed by the packaged vLLM image (cpu vs CUDA build). Current
	// vLLM CPU images reject --device, and CUDA images select the platform at
	// import time. Mode still gates whether load is allowed.
	_ = mode
	port := m.backend.Port()
	if port == "" {
		port = "8001"
	}
	return append(args, "--host", "127.0.0.1", "--port", port)
}

// Scope offline serving to the runtime child. The manager's catalog job and
// user-directed downloader still need their explicitly permitted network access.
func vllmProcessEnvironment(base []string) []string {
	// Numeric UIDs have no /etc/passwd entry under Restricted. Torch/vLLM call
	// getpass.getuser() during import unless USER/LOGNAME and cache dirs are set.
	const home = "/home/runtime"
	overrides := []string{
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
		"HF_HUB_DISABLE_TELEMETRY=1",
		"VLLM_NO_USAGE_STATS=1",
		"DO_NOT_TRACK=1",
		"HOME=" + home,
		"USER=runtime",
		"LOGNAME=runtime",
		"TORCHINDUCTOR_CACHE_DIR=" + filepath.Join(home, ".cache", "torch", "inductor"),
		"TRITON_CACHE_DIR=" + filepath.Join(home, ".cache", "triton"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, override := range overrides {
			replaced = replaced || strings.HasPrefix(override, key+"=")
		}
		if !replaced {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

func argumentPresent(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
}

func (m *manager) stopProcess() {
	m.processMu.Lock()
	generation := m.generation
	process := m.process
	done := m.processDone
	m.generation = ""
	m.active = ""
	m.process, m.processDone = nil, nil
	m.processMu.Unlock()

	if m.engine == "vllm" && m.controlDir != "" {
		_ = os.WriteFile(filepath.Join(m.controlDir, "generation"), []byte(""), 0o640)
		_ = os.Remove(filepath.Join(m.controlDir, "argv.json"))
		_ = generation
	}
	if process == nil || process.Process == nil {
		return
	}
	_ = process.Process.Signal(syscall.SIGTERM)
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = process.Process.Kill()
		<-done
	}
}

func directoryDigest(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".cache" && path != root {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		io.WriteString(h, filepath.ToSlash(rel))
		h.Write([]byte{0})
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "type": "inference_manager_error"}})
}
