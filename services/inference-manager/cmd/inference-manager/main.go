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
	backend     *url.URL
	proxy       *httputil.ReverseProxy
	mu          sync.Mutex
	processMu   sync.Mutex
	process     *exec.Cmd
	processDone chan struct{}
	active      string
	reg         registry
	download    func(context.Context, string, string) error
	start       func(context.Context, model) (*exec.Cmd, error)
	cudaProbe   func(context.Context) bool
}

func main() {
	engine := strings.ToLower(env("INFERENCE_ENGINE", "vllm"))
	modelsDir := env("INFERENCE_MODELS_DIR", "/models")
	backend, _ := url.Parse(env("INFERENCE_BACKEND_URL", "http://127.0.0.1:8001"))
	m := &manager{engine: engine, modelsDir: modelsDir, backend: backend, proxy: httputil.NewSingleHostReverseProxy(backend)}
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
	if engine == "ollama" {
		if err := m.startOllama(); err != nil {
			log.Fatalf("start Ollama: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", m.health)
	mux.HandleFunc("GET /internal/v1/runtime/capabilities", m.capabilities)
	mux.HandleFunc("POST /internal/v1/models/imports", m.importModel)
	mux.HandleFunc("POST /internal/v1/models/{model}/load", m.loadModel)
	mux.HandleFunc("DELETE /internal/v1/models/{model}", m.deleteModel)
	mux.HandleFunc("GET /v1/models", m.listModels)
	mux.HandleFunc("/v1/", m.proxyOpenAI)

	server := &http.Server{Addr: env("INFERENCE_LISTEN_ADDRESS", "0.0.0.0:11434"), Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		m.stopProcess()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("inference manager listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
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
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "engine": m.engine})
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
	cmd := exec.CommandContext(ctx, env("INFERENCE_PYTHON", "python3"), "-c", "import torch; raise SystemExit(0 if torch.cuda.is_available() else 1)")
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
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]model, 0, len(m.reg.Models))
	for _, item := range m.reg.Models {
		item.Path = ""
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
}

func (m *manager) importModel(w http.ResponseWriter, r *http.Request) {
	if !m.mu.TryLock() {
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	defer m.mu.Unlock()
	var req struct {
		ModelID         string
		Source          string
		Digest          string
		LaunchArguments []string `json:"launchArguments"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !modelRefRE.MatchString(req.ModelID) || !modelRefRE.MatchString(req.Source) {
		writeError(w, http.StatusBadRequest, "modelId and source must be repository model references")
		return
	}
	if _, exists := m.reg.Models[req.ModelID]; exists {
		writeError(w, http.StatusConflict, "model is already installed")
		return
	}
	if m.engine == "ollama" {
		m.importOllama(w, r, req.ModelID, req.Source, req.Digest)
		return
	}
	if err := validateVLLMArguments(req.LaunchArguments); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	keySum := sha256.Sum256([]byte(req.ModelID))
	key := hex.EncodeToString(keySum[:16])
	downloadRoot := filepath.Join(m.modelsDir, ".downloads")
	if err := os.MkdirAll(downloadRoot, 0o770); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp, err := os.MkdirTemp(downloadRoot, key+"-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tmp)
	if err := m.download(r.Context(), req.Source, tmp); err != nil {
		writeError(w, http.StatusBadGateway, "model download failed: "+err.Error())
		return
	}
	digest, err := directoryDigest(tmp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "model verification failed: "+err.Error())
		return
	}
	if req.Digest != "" && req.Digest != digest {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("model digest mismatch: got %s", digest))
		return
	}
	destination := filepath.Join(m.modelsDir, key)
	if err := os.Rename(tmp, destination); err != nil {
		writeError(w, http.StatusInternalServerError, "install model: "+err.Error())
		return
	}
	item := model{ID: req.ModelID, Object: "model", OwnedBy: "appliance", OpenAIOwnedBy: "appliance", Source: req.Source, Digest: digest, Path: destination, InstalledAt: time.Now().UTC(), LaunchArguments: append([]string(nil), req.LaunchArguments...)}
	m.reg.Models[item.ID] = item
	if err := m.saveRegistry(); err != nil {
		delete(m.reg.Models, item.ID)
		_ = os.RemoveAll(destination)
		writeError(w, http.StatusInternalServerError, "save model registry: "+err.Error())
		return
	}
	item.Path = ""
	writeJSON(w, http.StatusCreated, item)
}

func (m *manager) loadModel(w http.ResponseWriter, r *http.Request) {
	if !m.mu.TryLock() {
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	defer m.mu.Unlock()
	id := r.PathValue("model")
	if m.engine == "ollama" {
		if err := m.callBackend(r.Context(), http.MethodPost, "/api/generate", map[string]any{"model": id, "prompt": "", "stream": false, "keep_alive": "5m"}, nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"modelId": id, "state": "loaded"})
		return
	}
	item, ok := m.reg.Models[id]
	if !ok {
		writeError(w, http.StatusNotFound, "model is not installed")
		return
	}
	m.stopProcess()
	cmd, err := m.start(context.Background(), item)
	if err != nil {
		writeError(w, http.StatusBadGateway, "start vLLM: "+err.Error())
		return
	}
	done := make(chan struct{})
	m.processMu.Lock()
	m.process, m.processDone, m.active = cmd, done, id
	m.processMu.Unlock()
	go func() {
		err := cmd.Wait()
		m.processMu.Lock()
		if m.process == cmd {
			m.process, m.processDone, m.active = nil, nil, ""
		}
		m.processMu.Unlock()
		close(done)
		if err != nil {
			log.Printf("vLLM model %q exited: %v", id, err)
		}
	}()
	if err := m.waitBackend(r.Context(), done); err != nil {
		m.stopProcess()
		writeError(w, http.StatusBadGateway, "vLLM did not become ready: "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"modelId": id, "state": "loaded"})
}

func (m *manager) waitBackend(ctx context.Context, done <-chan struct{}) error {
	return m.waitBackendPath(ctx, done, "/health")
}

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
			return errors.New("runtime process exited")
		case <-deadline.C:
			return errors.New("startup timed out")
		case <-ticker.C:
		}
	}
}

func (m *manager) deleteModel(w http.ResponseWriter, r *http.Request) {
	if !m.mu.TryLock() {
		writeError(w, http.StatusConflict, "another model operation is in progress")
		return
	}
	defer m.mu.Unlock()
	id := r.PathValue("model")
	if m.engine == "ollama" {
		if err := m.callBackend(r.Context(), http.MethodDelete, "/api/delete", map[string]any{"model": id}, nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
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

func (m *manager) startOllama() error {
	cmd := exec.Command(env("INFERENCE_OLLAMA_COMMAND", "ollama"), "serve")
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+m.backend.Host, "OLLAMA_MODELS="+m.modelsDir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	m.processMu.Lock()
	m.process, m.processDone = cmd, done
	m.processMu.Unlock()
	go func() {
		err := cmd.Wait()
		m.processMu.Lock()
		if m.process == cmd {
			m.process, m.processDone = nil, nil
		}
		m.processMu.Unlock()
		close(done)
		if err != nil {
			log.Printf("Ollama exited: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return m.waitBackendPath(ctx, done, "/")
}

func (m *manager) startOllamaModel(context.Context, model) (*exec.Cmd, error) {
	return nil, errors.New("Ollama models are loaded through the running Ollama API")
}

func (m *manager) importOllama(w http.ResponseWriter, r *http.Request, modelID, source, digest string) {
	if modelID != source {
		writeError(w, http.StatusBadRequest, "Ollama modelId must equal source")
		return
	}
	if digest != "" {
		writeError(w, http.StatusUnprocessableEntity, "expected aggregate digest is not supported for Ollama pulls")
		return
	}
	if err := m.callBackend(r.Context(), http.MethodPost, "/api/pull", map[string]any{"model": source, "stream": false}, nil); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": modelID, "object": "model", "ownedBy": "ollama", "owned_by": "ollama"})
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
	return cmd.Run()
}

func (m *manager) startVLLM(_ context.Context, item model) (*exec.Cmd, error) {
	_, mode, _ := m.selectMode(context.Background())
	if mode == "" {
		return nil, errors.New("no usable inference mode")
	}
	applyVLLMRlimits()
	args := m.vllmCommandArguments(item, mode)
	cmd := exec.Command(env("INFERENCE_VLLM_COMMAND", "vllm"), args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (m *manager) vllmCommandArguments(item model, mode string) []string {
	args := []string{"serve", item.Path}
	args = append(args, item.LaunchArguments...)
	if !argumentPresent(item.LaunchArguments, "--served-model-name") {
		args = append(args, "--served-model-name", item.ID)
	}
	return append(args, "--host", "127.0.0.1", "--port", strings.TrimPrefix(m.backend.Port(), ":"), "--device", mode)
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
	if m.process == nil || m.process.Process == nil {
		m.process, m.processDone, m.active = nil, nil, ""
		m.processMu.Unlock()
		return
	}
	process := m.process.Process
	done := m.processDone
	_ = process.Signal(syscall.SIGTERM)
	m.processMu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = process.Kill()
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
