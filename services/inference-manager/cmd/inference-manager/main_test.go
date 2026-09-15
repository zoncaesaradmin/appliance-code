package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	for _, expected := range []string{"serve /models/model", "--quantization modelopt_fp4", "--max-model-len 262144", "--gpu-memory-utilization 0.8", "--cudagraph-capture-sizes 4", "--no-enable-flashinfer-autotune", "--enable-auto-tool-choice", "--served-model-name qwen3.6", "--tool-call-parser qwen3_coder", "--host 127.0.0.1", "--port 1", "--device cuda"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("command %q missing %q", joined, expected)
		}
	}
	for _, forbidden := range [][]string{{"--model", "/tmp/model"}, {"--host", "0.0.0.0"}, {"--port", "9000"}} {
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
	if w.Code != http.StatusCreated {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "tiny:latest") {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	if len(calls) != 2 || calls[0] != "POST /api/pull" || calls[1] != "GET /api/tags" {
		t.Fatalf("engine calls=%v", calls)
	}
}

func TestImportListDelete(t *testing.T) {
	m := testManager(t)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/imports", strings.NewReader(`{"ModelID":"test/tiny","Source":"test/tiny"}`))
	w := httptest.NewRecorder()
	m.importModel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}

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

	req = httptest.NewRequest(http.MethodDelete, "/internal/v1/models/test%2Ftiny", nil)
	req.SetPathValue("model", "test/tiny")
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
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("digest mismatch status %d: %s", w.Code, w.Body.String())
	}
	if len(m.reg.Models) != 0 {
		t.Fatal("digest-mismatched model was registered")
	}
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
	m.start = func(_ context.Context, _ model) (*exec.Cmd, error) {
		cmd := exec.Command("sh", "-c", "sleep 2")
		return cmd, cmd.Start()
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/test%2Ftiny/load", nil)
	req.SetPathValue("model", item.ID)
	w := httptest.NewRecorder()
	m.loadModel(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("load status %d: %s", w.Code, w.Body.String())
	}
}
