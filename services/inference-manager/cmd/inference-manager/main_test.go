package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T) *manager {
	t.Helper()
	dir := t.TempDir()
	backend, _ := url.Parse("http://127.0.0.1:1")
	m := &manager{modelsDir: dir, backend: backend, reg: registry{Models: map[string]model{}}}
	m.download = func(_ context.Context, source, destination string) error {
		return os.WriteFile(filepath.Join(destination, "config.json"), []byte(source), 0o660)
	}
	m.cudaProbe = func(context.Context) bool { return false }
	return m
}

func TestManagerHTTPRouting(t *testing.T) {
	for _, engine := range []string{"ollama", "vllm"} {
		t.Run(engine, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Backend-Request", r.Method+" "+r.URL.RequestURI())
				w.WriteHeader(http.StatusAccepted)
			}))
			defer backend.Close()
			m := testManager(t)
			m.engine = engine
			m.active = "test-model"
			m.backend, _ = url.Parse(backend.URL)
			m.proxy = httputil.NewSingleHostReverseProxy(m.backend)
			// Construct the same complete router used by main; conflicting
			// patterns panic here even if individual handler tests pass.
			handler := m.handler()
			for _, tc := range []struct {
				method, path string
				status       int
				proxied      bool
			}{
				{"GET", "/", http.StatusOK, false},
				{"HEAD", "/", http.StatusOK, false},
				{"POST", "/", http.StatusMethodNotAllowed, false},
				{"GET", "/unknown", http.StatusNotFound, false},
				{"GET", "/v1/models", http.StatusOK, false},
				{"POST", "/v1/chat/completions?stream=true", http.StatusAccepted, true},
				{"GET", "/v1/responses/example", http.StatusAccepted, true},
				{"DELETE", "/v1/responses/example", http.StatusAccepted, true},
			} {
				// Ollama's model list comes from /api/tags on its backend.
				if engine == "ollama" && tc.path == "/v1/models" {
					continue
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
				if response.Code != tc.status {
					t.Fatalf("%s %s: status %d, want %d: %s", tc.method, tc.path, response.Code, tc.status, response.Body.String())
				}
				if tc.proxied && response.Header().Get("X-Backend-Request") != tc.method+" "+tc.path {
					t.Fatalf("request was not forwarded intact: %s %s", tc.method, tc.path)
				}
			}
		})
	}
}

func TestAutoModePrefersConfirmedCUDA(t *testing.T) {
	m := testManager(t)
	m.cudaProbe = func(context.Context) bool { return true }
	t.Setenv("INFERENCE_MODE", "auto")
	t.Setenv("INFERENCE_SUPPORTED_MODES", "cpu,cuda")
	available, active, _ := m.selectMode(context.Background())
	if active != "cuda" || len(available) == 0 || available[0] != "cuda" {
		t.Fatalf("available=%v active=%q, want CUDA selected", available, active)
	}
}

func TestVLLMServingIsOfflineWithoutChangingManagerEnvironment(t *testing.T) {
	base := []string{"HF_HUB_OFFLINE=0", "VLLM_NO_USAGE_STATS=0", "HF_HOME=/models/.cache/huggingface", "CUDA_VISIBLE_DEVICES=0"}
	child := vllmProcessEnvironment(base)
	for _, want := range []string{
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
		"HF_HUB_DISABLE_TELEMETRY=1",
		"VLLM_NO_USAGE_STATS=1",
		"DO_NOT_TRACK=1",
		"USER=runtime",
		"LOGNAME=runtime",
		"HOME=/home/runtime",
		"TORCHINDUCTOR_CACHE_DIR=/home/runtime/.cache/torch/inductor",
		"TRITON_CACHE_DIR=/home/runtime/.cache/triton",
		"XDG_CACHE_HOME=/home/runtime/.cache",
		"HF_HOME=/models/.cache/huggingface",
		"CUDA_VISIBLE_DEVICES=0",
	} {
		count := 0
		for _, entry := range child {
			if entry == want {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("child environment must contain %q exactly once: %v", want, child)
		}
	}
	for _, entry := range child {
		if entry == "HF_HUB_OFFLINE=0" || entry == "VLLM_NO_USAGE_STATS=0" {
			t.Fatalf("conflicting override retained: %s", entry)
		}
	}
	if base[0] != "HF_HUB_OFFLINE=0" {
		t.Fatal("manager environment was mutated")
	}
}

func TestAutoModeFallsBackToCPUWhenCUDANotConfirmed(t *testing.T) {
	m := testManager(t)
	t.Setenv("INFERENCE_MODE", "auto")
	t.Setenv("INFERENCE_SUPPORTED_MODES", "cpu,cuda")
	_, active, _ := m.selectMode(context.Background())
	if active != "cpu" {
		t.Fatalf("active=%q, want CPU fallback", active)
	}
}

func TestValidatedVLLMArgumentsMatchSupportedDockerInvocation(t *testing.T) {
	arguments := []string{
		"--quantization", "modelopt_fp4",
		"--max-model-len", "262144",
		"--gpu-memory-utilization", "0.8",
		"--cudagraph-capture-sizes", "4",
		"--no-enable-flashinfer-autotune",
		"--enable-auto-tool-choice",
		"--served-model-name", "qwen3.6",
		"--tool-call-parser", "qwen3_coder",
	}
	if err := validateVLLMArguments(arguments); err != nil {
		t.Fatalf("validate exact vLLM arguments: %v", err)
	}
	m := testManager(t)
	got := m.vllmCommandArguments(model{ID: "internal", Path: "/models/model", LaunchArguments: arguments}, "cuda")
	joined := strings.Join(got, " ")
	for _, expected := range []string{"serve /models/model", "--quantization modelopt_fp4", "--max-model-len 262144", "--gpu-memory-utilization 0.8", "--cudagraph-capture-sizes 4", "--no-enable-flashinfer-autotune", "--enable-auto-tool-choice", "--served-model-name qwen3.6", "--tool-call-parser qwen3_coder", "--host 127.0.0.1", "--port 1"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("command %q missing %q", joined, expected)
		}
	}
	if strings.Contains(joined, "--device") {
		t.Fatalf("command unexpectedly includes deprecated --device: %q", joined)
	}
	cpuJoined := strings.Join(m.vllmCommandArguments(model{ID: "cpu-model", Path: "/models/cpu"}, "cpu"), " ")
	if strings.Contains(cpuJoined, "--device") {
		t.Fatalf("cpu command unexpectedly includes --device: %q", cpuJoined)
	}
	for _, forbidden := range [][]string{{"--model", "/tmp/model"}, {"--host", "0.0.0.0"}, {"--port", "9000"}, {"--device", "cpu"}} {
		if err := validateVLLMArguments(forbidden); err == nil {
			t.Fatalf("manager-owned argument %v was accepted", forbidden)
		}
	}
}

func TestOllamaUsesUnifiedManagerLifecycle(t *testing.T) {
	var calls []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "tiny:latest"}}})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	m := testManager(t)
	m.engine = "ollama"
	m.backend, _ = url.Parse(backend.URL)

	w := httptest.NewRecorder()
	m.importModel(w, httptest.NewRequest(http.MethodPost, "/internal/v1/models/imports", strings.NewReader(`{"ModelID":"tiny:latest","Source":"tiny:latest"}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	waitImportState(t, m, "complete")
	w = httptest.NewRecorder()
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "tiny:latest") {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	if len(calls) < 1 || calls[0] != "POST /api/pull" {
		t.Fatalf("engine calls=%v", calls)
	}
}

func TestImportListDelete(t *testing.T) {
	m := testManager(t)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/imports", strings.NewReader(`{"ModelID":"test/tiny","Source":"test/tiny"}`))
	w := httptest.NewRecorder()
	m.importModel(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	waitImportState(t, m, "complete")

	w = httptest.NewRecorder()
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	var listed struct {
		Data []model `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Data) != 1 || listed.Data[0].ID != "test/tiny" {
		t.Fatalf("unexpected list: %+v, err=%v", listed, err)
	}
	if listed.Data[0].Path != "" {
		t.Fatal("internal model path leaked")
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/v1/models/delete", strings.NewReader(`{"modelId":"test/tiny"}`))
	w = httptest.NewRecorder()
	m.deleteModel(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}
}

func TestImportExpectedDigestFailsClosed(t *testing.T) {
	m := testManager(t)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/imports", strings.NewReader(`{"ModelID":"test/tiny","Source":"test/tiny","Digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	w := httptest.NewRecorder()
	m.importModel(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("digest mismatch accept status %d: %s", w.Code, w.Body.String())
	}
	waitImportState(t, m, "failed")
	if len(m.reg.Models) != 0 {
		t.Fatal("digest-mismatched model was registered")
	}
}

func TestImportProgressEndpoint(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{})
	release := make(chan struct{})
	m.download = func(_ context.Context, source, destination string) error {
		close(started)
		<-release
		return os.WriteFile(filepath.Join(destination, "config.json"), []byte(source), 0o660)
	}
	w := httptest.NewRecorder()
	m.importModel(w, httptest.NewRequest(http.MethodPost, "/internal/v1/models/imports", strings.NewReader(`{"ModelID":"test/tiny","Source":"test/tiny"}`)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	<-started
	w = httptest.NewRecorder()
	m.handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models/imports/progress", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"downloading"`) {
		t.Fatalf("progress status %d: %s", w.Code, w.Body.String())
	}
	close(release)
	waitImportState(t, m, "complete")
}

func waitImportState(t *testing.T, m *manager, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := m.currentImportProgress().State
		if state == want {
			return
		}
		if state == "failed" && want != "failed" {
			t.Fatalf("import failed: %+v", m.currentImportProgress())
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("import state=%q, want %q (%+v)", m.currentImportProgress().State, want, m.currentImportProgress())
}

func TestNoLoadedModelFailsOpenAIRequest(t *testing.T) {
	m := testManager(t)
	w := httptest.NewRecorder()
	m.proxyOpenAI(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadStartsInstalledModel(t *testing.T) {
	m := testManager(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer backend.Close()
	m.backend, _ = url.Parse(backend.URL)
	item := model{ID: "test/tiny", Path: filepath.Join(m.modelsDir, "tiny")}
	m.reg.Models[item.ID] = item
	started := make(chan struct{})
	m.start = func(_ context.Context, _ model) error {
		close(started)
		return nil
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/load", strings.NewReader(`{"modelId":"test/tiny"}`))
	w := httptest.NewRecorder()
	m.loadModel(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("load status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"state":"loading"`) {
		t.Fatalf("expected async loading accept: %s", w.Body.String())
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected start callback for loaded model")
	}
	waitLoadState(t, m, "ready")
	w = httptest.NewRecorder()
	m.handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models/load/progress", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"ready"`) {
		t.Fatalf("progress status %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadAlreadyReadyIsIdempotent(t *testing.T) {
	m := testManager(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer backend.Close()
	m.backend, _ = url.Parse(backend.URL)
	m.reg.Models["test/tiny"] = model{ID: "test/tiny", Path: filepath.Join(m.modelsDir, "tiny")}
	m.active = "test/tiny"
	m.finishLoadProgress("ready", "test/tiny", "Model is ready for use")
	started := false
	m.start = func(context.Context, model) error {
		started = true
		return nil
	}
	w := httptest.NewRecorder()
	m.loadModel(w, httptest.NewRequest(http.MethodPost, "/internal/v1/models/load", strings.NewReader(`{"modelId":"test/tiny"}`)))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"state":"ready"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if started {
		t.Fatal("already-ready load restarted the engine")
	}
}

func waitLoadState(t *testing.T, m *manager, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := m.currentLoadProgress().State
		if state == want {
			return
		}
		if state == "failed" && want != "failed" {
			t.Fatalf("load failed: %+v", m.currentLoadProgress())
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("load state=%q, want %q (%+v)", m.currentLoadProgress().State, want, m.currentLoadProgress())
}
