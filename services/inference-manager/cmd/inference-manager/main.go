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
	ID              string            `json:"id"`
	SizeBytes       uint64            `json:"sizeBytes,omitempty"`
	Object          string            `json:"object"`
	OwnedBy         string            `json:"ownedBy"`
	OpenAIOwnedBy   string            `json:"owned_by"`
	Source          string            `json:"source"`
	Digest          string            `json:"digest"`
	Path            string            `json:"path"`
	InstalledAt     time.Time         `json:"installedAt"`
	LaunchArguments []string          `json:"launchArguments,omitempty"`
	Capabilities    modelCapabilities `json:"capabilities"`
}

// modelCapabilities is an appliance assertion, not a claim inferred from a
// model name. A model must be explicitly imported with a compatible runtime
// profile before it is offered to agent clients.
type modelCapabilities struct {
	Experiences         []string `json:"experiences"`
	ToolCalling         bool     `json:"toolCalling"`
	ResponsesCompatible bool     `json:"responsesCompatible"`
	CodexCompatible     bool     `json:"codexCompatible"`
	Verification        string   `json:"verification"`
}

func chatCapabilities() modelCapabilities {
	return modelCapabilities{Experiences: []string{"chat"}, Verification: "chat-only"}
}

func unknownCapabilities() modelCapabilities {
	return modelCapabilities{Experiences: []string{"chat"}, Verification: "unverified"}
}

// ollamaCapabilities reflects the runtime's per-model declaration from
// /api/show. Ollama is not intrinsically chat-only: a model that advertises
// tools can be used by an agent client through the appliance Responses proxy.
// A failed discovery remains chat-only rather than guessing from a model name.
func ollamaCapabilities(capabilities []string, template string) modelCapabilities {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), "tools") {
			return toolCapableOllamaModel("runtime-reported")
		}
	}
	// Older packaged Ollama versions do not include capabilities in /api/show.
	// Their model templates still expose the supported tool surface. This checks
	// only template actions (not a model name), so an ordinary code-completion
	// model is not accidentally promoted to an agent.
	if strings.Contains(template, ".Tools") || strings.Contains(template, "$.Tools") {
		return toolCapableOllamaModel("template-reported")
	}
	return chatCapabilities()
}

func toolCapableOllamaModel(verification string) modelCapabilities {
	return modelCapabilities{
		Experiences:         []string{"chat", "coding-agent"},
		ToolCalling:         true,
		ResponsesCompatible: true,
		CodexCompatible:     true,
		Verification:        verification,
	}
}

func configuredAgentCapabilities(engine string, arguments []string) modelCapabilities {
	if engine != "vllm" || !hasVLLMToolCalling(arguments) {
		return chatCapabilities()
	}
	return modelCapabilities{
		Experiences:         []string{"chat", "coding-agent"},
		ToolCalling:         true,
		ResponsesCompatible: true,
		CodexCompatible:     true,
		Verification:        "configured",
	}
}

func hasVLLMToolCalling(arguments []string) bool {
	hasAuto, hasParser := false, false
	for i := 0; i < len(arguments); i++ {
		switch strings.TrimSpace(arguments[i]) {
		case "--enable-auto-tool-choice":
			hasAuto = true
		case "--tool-call-parser":
			hasParser = i+1 < len(arguments) && strings.TrimSpace(arguments[i+1]) != ""
		}
	}
	return hasAuto && hasParser
}

type registry struct {
	Models map[string]model `json:"models"`
}

type manager struct {
	engine     string
	modelsDir  string
	backend    *url.URL
	proxy      *httputil.ReverseProxy
	engineOrch engineOrchestrator
	mu         sync.RWMutex // protects persistent registry and Ollama inventory cache
	opMu       sync.Mutex   // single-flight import/load/delete (held for async import/load lifetime)
	progressMu sync.RWMutex
	progress   importProgress
	loadMu     sync.RWMutex
	load       loadProgress
	processMu  sync.Mutex
	active     string
	reg        registry
	ollama     ollamaInventory
	instanceMu sync.RWMutex
	instances  instanceRegistry
	download   func(context.Context, string, string) error
	gpuProbe   func(context.Context) bool
	catalog    *modelCatalog
}

type importRequest struct {
	ModelID         string
	Source          string
	Digest          string
	LaunchArguments []string
	CatalogID       string
	BytesTotal      uint64
	Capabilities    modelCapabilities
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine := strings.ToLower(env("INFERENCE_ENGINE", "vllm"))
	modelsDir := env("INFERENCE_MODELS_DIR", "/models")
	backend, _ := url.Parse(env("INFERENCE_BACKEND_URL", "http://inference-engine.inference.svc.cluster.local:8001"))
	orch, err := newEngineOrchestratorFromEnv(engine)
	if err != nil {
		log.Fatalf("engine orchestrator: %v", err)
	}
	m := &manager{
		engine:     engine,
		modelsDir:  modelsDir,
		backend:    backend,
		proxy:      newOpenAIReverseProxy(backend),
		engineOrch: orch,
	}
	m.download = m.downloadModel
	m.gpuProbe = gpuAvailable
	if err := m.loadRegistry(); err != nil {
		log.Fatalf("load model registry: %v", err)
	}
	if err := m.loadOllamaInventory(); err != nil {
		log.Fatalf("load Ollama model inventory: %v", err)
	}
	if err := m.loadInstanceRegistry(); err != nil {
		log.Fatalf("load instance registry: %v", err)
	}
	m.reconcileInterruptedProgress()
	if err := m.migrateLegacyDefaultInstance(); err != nil {
		log.Fatalf("migrate legacy default instance: %v", err)
	}
	// Start discovery and runtime reconciliation at process start. Both run
	// behind the API: cached catalog and inventory must be readable immediately,
	// even while the engine or upstream registry is slow or unavailable.
	m.catalog = newModelCatalog(m)
	go m.catalog.run(ctx)
	m.startRuntimeInitialization(ctx)

	server := &http.Server{Addr: env("INFERENCE_LISTEN_ADDRESS", "0.0.0.0:11434"), Handler: m.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("inference manager listening on %s (engine=%s)", server.Addr, engine)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// startRuntimeInitialization serializes startup reconciliation with model
// mutations but never delays read-only API requests. The local inventory cache
// was already loaded from the PVC before this begins.
func (m *manager) startRuntimeInitialization(ctx context.Context) {
	m.opMu.Lock()
	go func() {
		defer m.opMu.Unlock()
		if err := m.engineOrch.EnsureService(ctx); err != nil {
			log.Printf("ensure engine service: %v", err)
		}
		m.rehydrateActiveModel(ctx)
		if m.engine == "ollama" {
			inventoryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			m.refreshOllamaInventory(inventoryCtx)
		}
	}()
}

func (m *manager) handler() http.Handler {
	mux := http.NewServeMux()
	// Manager-owned admin/control surface (never proxy these to the engine):
	// health, capabilities, downloaded inventory, catalog, import/load/delete.
	mux.HandleFunc("GET /{$}", m.health)
	mux.HandleFunc("GET /internal/v1/runtime/capabilities", m.capabilities)
	mux.HandleFunc("POST /internal/v1/models/imports", m.importModel)
	mux.HandleFunc("GET /internal/v1/models/imports/progress", m.importProgress)
	// Body-based actions: Hugging Face ids contain "/", which breaks single-segment path params.
	mux.HandleFunc("POST /internal/v1/models/load", m.loadModel)
	mux.HandleFunc("GET /internal/v1/models/load/progress", m.loadProgressHandler)
	mux.HandleFunc("POST /internal/v1/models/delete", m.deleteModel)
	mux.HandleFunc("GET /internal/v1/models", m.listModels)
	mux.HandleFunc("GET /internal/v1/models/catalog", m.modelCatalog)
	// OpenAI-compatible client surface only: blind-proxy /v1/* to the engine
	// (vLLM/Ollama), including GET /v1/models for the currently served model.
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
	payload := map[string]any{
		"status":        "ok",
		"engine":        m.engine,
		"loadedModelId": loadedModelID,
		"servingState":  servingState,
		"instances":     m.instanceSummaries(),
	}
	if servingState == "ready" {
		if maxLen := m.servedMaxModelLen(context.Background()); maxLen > 0 {
			payload["maxModelLen"] = maxLen
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

// servedMaxModelLen returns the context window the live engine is actually
// serving. Prefer the engine OpenAI /v1/models max_model_len; fall back to the
// persisted launch args from the last successful Load.
func (m *manager) servedMaxModelLen(ctx context.Context) uint64 {
	if m.engine == "vllm" {
		if maxLen := m.probeEngineMaxModelLen(ctx); maxLen > 0 {
			return maxLen
		}
	}
	m.processMu.Lock()
	active := m.active
	m.processMu.Unlock()
	if active == "" {
		return 0
	}
	m.mu.RLock()
	item, ok := m.reg.Models[active]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return maxModelLenFromArgs(item.LaunchArguments)
}

func (m *manager) probeEngineMaxModelLen(ctx context.Context) uint64 {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, m.backend.ResolveReference(&url.URL{Path: "/v1/models"}).String(), nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0
	}
	var body struct {
		Data []struct {
			MaxModelLen uint64 `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0
	}
	for _, item := range body.Data {
		if item.MaxModelLen > 0 {
			return item.MaxModelLen
		}
	}
	return 0
}

func (m *manager) capabilities(w http.ResponseWriter, _ *http.Request) {
	usingGPU, checks := m.resolveDevice(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"gpuAvailable": usingGPU, "checks": checks})
}

// resolveDevice decides whether a host GPU is usable for this package.
// Accelerated (vLLM) packages require a GPU. Standard (Ollama) packages may
// use a GPU when present, otherwise CPU.
func (m *manager) resolveDevice(ctx context.Context) (bool, []map[string]string) {
	checks := make([]map[string]string, 0, 2)
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	gpuOK := m.gpuProbe(probeCtx)
	cancel()
	if gpuOK {
		checks = append(checks, map[string]string{"name": "gpu", "status": "pass", "message": "GPU backend and visible device confirmed"})
	} else {
		checks = append(checks, map[string]string{"name": "gpu", "status": "fail", "message": "GPU backend or visible device is unavailable"})
	}
	if m.engine == "vllm" {
		if !gpuOK {
			checks = append(checks, map[string]string{"name": "accelerated-runtime", "status": "fail", "message": "accelerated inference requires a usable GPU"})
			return false, checks
		}
		checks = append(checks, map[string]string{"name": "accelerated-runtime", "status": "pass", "message": "vLLM with GPU"})
		return true, checks
	}
	// Standard Ollama: GPU optional; CPU always acceptable when AVX2 (amd64) or non-x86.
	if runtime.GOARCH == "amd64" && !cpuHasFeature("avx2") {
		checks = append(checks, map[string]string{"name": "cpu-runtime", "status": "fail", "message": "x86 CPU does not report AVX2"})
		return false, checks
	}
	checks = append(checks, map[string]string{"name": "standard-runtime", "status": "pass", "message": runtime.GOARCH})
	return gpuOK, checks
}

func (m *manager) deviceLabel(ctx context.Context) string {
	usingGPU, _ := m.resolveDevice(ctx)
	if usingGPU {
		return "gpu"
	}
	return "cpu"
}

func gpuAvailable(ctx context.Context) bool {
	// Prefer an explicit install-time GPU enablement signal, then fall back to
	// a visible NVIDIA device node. Product copy says "GPU", not a vendor mode.
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
		if len(item.Capabilities.Experiences) == 0 {
			item.Capabilities = configuredAgentCapabilities(m.engine, item.LaunchArguments)
		}
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
		job.Capabilities = entry.Capabilities
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
	capabilities := req.Capabilities
	if len(capabilities.Experiences) == 0 {
		capabilities = configuredAgentCapabilities(m.engine, req.LaunchArguments)
	}
	item := model{ID: req.ModelID, Object: "model", OwnedBy: "appliance", OpenAIOwnedBy: "appliance", Source: req.Source, Digest: digest, Path: destination, InstalledAt: time.Now().UTC(), LaunchArguments: append([]string(nil), req.LaunchArguments...), Capabilities: capabilities}
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
	if err := m.setDefaultInstance(id); err != nil {
		m.opMu.Unlock()
		writeError(w, http.StatusInternalServerError, "persist default model instance: "+err.Error())
		return
	}
	m.beginLoadProgress(id, "Loading model into the inference engine")
	go m.runLoad(context.Background(), id)
	writeJSON(w, http.StatusAccepted, m.currentLoadProgress())
}

func (m *manager) runLoad(ctx context.Context, id string) {
	defer m.opMu.Unlock()
	if m.engine == "ollama" {
		if err := m.deployOllamaEngine(ctx, id); err != nil {
			m.finishLoadProgress("failed", id, err.Error())
			return
		}
		if err := m.callBackend(ctx, http.MethodPost, "/api/generate", map[string]any{"model": id, "prompt": "", "stream": false, "keep_alive": "5m"}, nil); err != nil {
			_ = m.engineOrch.DeleteEngine(ctx)
			clearEngineDesire(m.modelsDir)
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
	if err := m.deployVLLMEngine(ctx, item); err != nil {
		m.finishLoadProgress("failed", id, err.Error())
		return
	}
	m.processMu.Lock()
	m.active = id
	m.processMu.Unlock()
	m.finishLoadProgress("ready", id, "Model is ready for use")
}

func (m *manager) deployVLLMEngine(ctx context.Context, item model) error {
	usingGPU, _ := m.resolveDevice(ctx)
	if !usingGPU {
		return errors.New("accelerated inference requires a usable GPU")
	}
	mode := "gpu"
	memoryBytes := m.memoryBytesForModel(ctx, item.ID, item.Path)
	budget, _ := m.catalogBudget(ctx)
	arch := modelArchFromModelDir(item.Path)
	cardLimit := arch.MaxPosition
	if cardLimit == 0 {
		cardLimit = maxModelLenFromArgs(item.LaunchArguments)
	}
	serve, err := planServe(serveWindowInput{
		Engine:             m.engine,
		Mode:               mode,
		ModelEstimateBytes: memoryBytes,
		AvailableBytes:     budget,
		AvailableCPUs:      hostCPUCount(),
		ModelContextLimit:  cardLimit,
		KVBytesPerToken:    arch.kvBytesPerToken(),
	})
	if err != nil {
		return err
	}
	spec, err := engineResourceSpecFromPlan(serve)
	if err != nil {
		return err
	}
	spec.ModelID = item.ID
	spec.Command = []string{env("INFERENCE_VLLM_COMMAND", "vllm")}
	effectiveLaunch := applyServeWindowMaxModelLen(item.LaunchArguments, serve.MaxModelLen, cardLimit)
	if serve.MaxModelLen == 0 {
		effectiveLaunch = clampLaunchMaxModelLen(item.LaunchArguments, item.Path)
	}
	spec.Args = m.vllmCommandArguments(model{
		ID:              item.ID,
		Path:            item.Path,
		LaunchArguments: effectiveLaunch,
	}, mode)
	if err := m.persistEffectiveLaunchArguments(item.ID, effectiveLaunch); err != nil {
		return err
	}
	m.setLoadProgressMessage("Creating inference engine Deployment")
	if err := m.replaceEngine(ctx, spec); err != nil {
		return err
	}
	_ = persistEngineDesire(m.modelsDir, item.ID, spec)
	m.setLoadProgressMessage("Waiting for inference engine to become ready")
	if err := m.waitEngineDeployment(ctx, 10*time.Minute); err != nil {
		_ = m.engineOrch.DeleteEngine(ctx)
		clearEngineDesire(m.modelsDir)
		m.processMu.Lock()
		m.active = ""
		m.processMu.Unlock()
		return err
	}
	return nil
}

func (m *manager) deployOllamaEngine(ctx context.Context, id string) error {
	memoryBytes := m.memoryBytesForModel(ctx, id, "")
	if memoryBytes == 0 {
		memoryBytes = 2 << 30
	}
	budget, _ := m.catalogBudget(ctx)
	spec, err := engineResourceSpec(m.engine, memoryBytes, budget)
	if err != nil {
		return err
	}
	spec.ModelID = id
	spec.Command = []string{"ollama"}
	spec.Args = []string{"serve"}
	m.setLoadProgressMessage("Creating inference engine Deployment")
	if err := m.replaceEngine(ctx, spec); err != nil {
		return err
	}
	_ = persistEngineDesire(m.modelsDir, id, spec)
	m.setLoadProgressMessage("Waiting for inference engine to become ready")
	if err := m.waitEngineDeployment(ctx, 10*time.Minute); err != nil {
		_ = m.engineOrch.DeleteEngine(ctx)
		clearEngineDesire(m.modelsDir)
		m.processMu.Lock()
		m.active = ""
		m.processMu.Unlock()
		return err
	}
	return nil
}

func (m *manager) replaceEngine(ctx context.Context, spec enginePodSpec) error {
	if err := m.engineOrch.EnsureService(ctx); err != nil {
		return fmt.Errorf("ensure engine service: %w", err)
	}
	if err := m.engineOrch.DeleteEngine(ctx); err != nil {
		return fmt.Errorf("delete previous engine: %w", err)
	}
	if err := m.engineOrch.ApplyEngine(ctx, spec); err != nil {
		return fmt.Errorf("apply engine deployment: %w", err)
	}
	return nil
}

func (m *manager) waitEngineDeployment(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := m.engineOrch.EngineStatus(ctx)
		if err != nil {
			return err
		}
		m.setLoadEngineStatus(status)
		if status.Ready {
			if m.backendHealthy(ctx) {
				return nil
			}
			m.setLoadProgressMessage("Engine pod ready; waiting for OpenAI health")
		}
		if status.OOMKilled {
			return fmt.Errorf("engine pod OOMKilled: %s", status.Message)
		}
		if time.Now().After(deadline) {
			if status.Message != "" {
				return fmt.Errorf("engine not ready: %s", status.Message)
			}
			return errors.New("engine not ready: timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *manager) memoryBytesForModel(ctx context.Context, id, path string) uint64 {
	if m.catalog != nil {
		for _, item := range m.catalog.snapshot(ctx).Items {
			if item.ID == id && item.MemoryBytes > 0 {
				return item.MemoryBytes
			}
		}
	}
	if m.engine == "ollama" {
		for _, item := range m.ollamaInventorySnapshot() {
			if item.ID == id && item.SizeBytes > 0 {
				return ollamaMemoryEstimate(item.SizeBytes)
			}
		}
	}
	if path == "" {
		return 0
	}
	var size uint64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > 0 {
			size += uint64(info.Size())
		}
		return nil
	})
	if size == 0 {
		return 0
	}
	return size*2 + (4 << 30)
}

func (m *manager) ensureOllamaDaemon(ctx context.Context) error {
	status, err := m.engineOrch.EngineStatus(ctx)
	if err != nil {
		return err
	}
	if status.Ready && m.backendHealthy(ctx) {
		return nil
	}
	m.processMu.Lock()
	active := m.active
	m.processMu.Unlock()
	if active != "" {
		// Never tear down a loaded engine from inventory/list paths.
		return m.waitEngineDeployment(ctx, 2*time.Minute)
	}
	spec, err := engineResourceSpec(m.engine, 2<<30, 0)
	if err != nil {
		spec = idleOllamaResources()
	}
	spec.ModelID = ""
	spec.Command = []string{"ollama"}
	spec.Args = []string{"serve"}
	if err := m.replaceEngine(ctx, spec); err != nil {
		return err
	}
	return m.waitEngineDeployment(ctx, 5*time.Minute)
}

func (m *manager) isModelReady(ctx context.Context, id string) bool {
	m.processMu.Lock()
	active := m.active == id
	m.processMu.Unlock()
	if !active {
		return false
	}
	status, err := m.engineOrch.EngineStatus(ctx)
	if err != nil || !status.Ready {
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
		if err := m.ensureOllamaDaemon(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if err := m.callBackend(r.Context(), http.MethodDelete, "/api/delete", map[string]any{"model": id}, nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		m.processMu.Lock()
		active := m.active == id
		m.processMu.Unlock()
		if active {
			_ = m.engineOrch.DeleteEngine(r.Context())
			clearEngineDesire(m.modelsDir)
			m.processMu.Lock()
			m.active = ""
			m.processMu.Unlock()
			m.finishLoadProgress("idle", "", "No model is loaded")
		}
		m.refreshOllamaInventory(r.Context())
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
		_ = m.engineOrch.DeleteEngine(r.Context())
		clearEngineDesire(m.modelsDir)
		m.processMu.Lock()
		m.active = ""
		m.processMu.Unlock()
		m.finishLoadProgress("idle", "", "No model is loaded")
	}
	if err := m.clearDefaultInstance(id); err != nil {
		writeError(w, http.StatusInternalServerError, "clear default model instance: "+err.Error())
		return
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
	if active == "" {
		writeError(w, http.StatusServiceUnavailable, "no model is loaded")
		return
	}
	status, err := m.engineOrch.EngineStatus(r.Context())
	if err != nil || !status.Exists || !status.Ready {
		message := "no model is loaded"
		if status.OOMKilled {
			message = "inference engine OOMKilled"
		} else if status.Message != "" {
			message = "inference engine unavailable: " + status.Message
		} else if err != nil {
			message = "inference engine unavailable"
		}
		writeError(w, http.StatusServiceUnavailable, message)
		return
	}
	usingGPU, _ := m.resolveDevice(r.Context())
	if err := prepareOpenAIProxyRequest(r, openaiCompatOptions(usingGPU)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid OpenAI request body: "+err.Error())
		return
	}
	m.proxy.ServeHTTP(w, r)
}

func (m *manager) runOllamaImport(ctx context.Context, req importRequest) {
	if err := m.ensureOllamaDaemon(ctx); err != nil {
		m.finishImportProgress("failed", err.Error())
		return
	}
	if err := m.pullOllamaModel(ctx, req.Source, 2*time.Minute); err != nil {
		m.finishImportProgress("failed", err.Error())
		return
	}
	m.setImportProgressState("installing", "Updating downloaded model list")
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	m.refreshOllamaInventory(refreshCtx)
	cancel()
	m.finishImportProgress("complete", "Model downloaded")
}

func (m *manager) listOllamaModels(w http.ResponseWriter) {
	// Serve the persisted snapshot immediately. Live discovery happens at pod
	// startup and after lifecycle changes, never on this UI/API read path.
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": m.ollamaInventorySnapshot()})
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

func (m *manager) persistEffectiveLaunchArguments(modelID string, arguments []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.reg.Models[modelID]
	if !ok {
		return nil
	}
	item.LaunchArguments = append([]string(nil), arguments...)
	m.reg.Models[modelID] = item
	return m.saveRegistry()
}

func (m *manager) vllmCommandArguments(item model, mode string) []string {
	args := []string{"serve", item.Path}
	// LaunchArguments are already planned by deployVLLMEngine / catalog snapshot
	// (model card ∩ memory KV ∩ mode prefill). Do not re-expand to the raw card.
	args = append(args, item.LaunchArguments...)
	args = ensureVLLMLaunchDefaults(args, mode)
	if !argumentPresent(args, "--served-model-name") {
		args = append(args, "--served-model-name", item.ID)
	}
	// Device is fixed by the packaged vLLM image (cpu vs CUDA build). Current
	// vLLM CPU images reject --device, and CUDA images select the platform at
	// import time. Mode still gates whether load is allowed via resolveDevice.
	port := env("INFERENCE_ENGINE_PORT", "")
	if port == "" {
		port = m.backend.Port()
	}
	if port == "" {
		port = "8001"
	}
	return append(args, "--host", "0.0.0.0", "--port", port)
}

// ensureVLLMLaunchDefaults aligns launch argv with the known-good docker
// contract used on appliance GPUs (gpu-memory-utilization 0.8). CPU mode
// leaves utilization unset (vLLM CPU builds reject the flag).
func ensureVLLMLaunchDefaults(arguments []string, mode string) []string {
	out := append([]string(nil), arguments...)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "gpu", "cuda":
		if !argumentPresent(out, "--gpu-memory-utilization") {
			util := strings.TrimSpace(env("INFERENCE_VLLM_GPU_MEMORY_UTILIZATION", "0.8"))
			if util == "" {
				util = "0.8"
			}
			out = append(out, "--gpu-memory-utilization", util)
		}
	}
	return out
}

func argumentPresent(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
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
