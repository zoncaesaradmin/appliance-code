package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

func testManager(t *testing.T) *manager {
	t.Helper()
	dir := t.TempDir()
	backend, _ := url.Parse("http://127.0.0.1:1")
	fake := &fakeEngine{ready: true, exists: true, phase: "Running"}
	m := &manager{
		modelsDir:  dir,
		backend:    backend,
		engineOrch: fake,
		reg:        registry{Models: map[string]model{}},
	}
	m.download = func(_ context.Context, source, destination string) error {
		return os.WriteFile(filepath.Join(destination, "config.json"), []byte(source), 0o660)
	}
	m.gpuProbe = func(context.Context) bool { return false }
	return m
}

func TestManagerHTTPRouting(t *testing.T) {
	for _, engine := range []string{"ollama", "vllm"} {
		t.Run(engine, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{}})
					return
				}
				w.Header().Set("X-Backend-Request", r.Method+" "+r.URL.RequestURI())
				w.WriteHeader(http.StatusAccepted)
			}))
			defer backend.Close()
			m := testManager(t)
			m.engine = engine
			m.active = "test-model"
			m.backend, _ = url.Parse(backend.URL)
			m.proxy = newOpenAIReverseProxy(m.backend)
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
				{"GET", "/internal/v1/models", http.StatusOK, false},
				{"GET", "/internal/v1/models/catalog", http.StatusServiceUnavailable, false},
				{"GET", "/internal/v1/runtime/capabilities", http.StatusOK, false},
				{"GET", "/v1/models", http.StatusAccepted, true},
				{"POST", "/v1/chat/completions?stream=true", http.StatusAccepted, true},
				{"GET", "/v1/responses/example", http.StatusAccepted, true},
				{"DELETE", "/v1/responses/example", http.StatusAccepted, true},
			} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
				if response.Code != tc.status {
					t.Fatalf("%s %s: status %d, want %d: %s", tc.method, tc.path, response.Code, tc.status, response.Body.String())
				}
				if tc.proxied && response.Header().Get("X-Backend-Request") != tc.method+" "+tc.path {
					t.Fatalf("request was not forwarded intact: %s %s", tc.method, tc.path)
				}
				if !tc.proxied && response.Header().Get("X-Backend-Request") != "" {
					t.Fatalf("manager-owned path was proxied: %s %s", tc.method, tc.path)
				}
			}
		})
	}
}

func TestResolveDevicePrefersConfirmedGPU(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	m.gpuProbe = func(context.Context) bool { return true }
	usingGPU, checks := m.resolveDevice(context.Background())
	if !usingGPU {
		t.Fatalf("usingGPU=false checks=%v", checks)
	}
	if m.deviceLabel(context.Background()) != "gpu" {
		t.Fatalf("deviceLabel=%q", m.deviceLabel(context.Background()))
	}
}

func TestVLLMEnginePodSetsOfflineEnvContract(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi")
	spec, err := engineResourceSpec("vllm", 1<<30, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	spec.ModelID = "test/model"
	spec.Command = []string{"vllm"}
	spec.Args = []string{"serve", "/models/x", "--host", "0.0.0.0", "--port", "8001"}
	if spec.MemoryLimit.Cmp(resource.MustParse("1Gi")) < 0 {
		t.Fatalf("unexpected memory limit %s", spec.MemoryLimit.String())
	}
}

func TestResolveDeviceRejectsAcceleratedWithoutGPU(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	m.gpuProbe = func(context.Context) bool { return false }
	usingGPU, checks := m.resolveDevice(context.Background())
	if usingGPU {
		t.Fatalf("usingGPU=true checks=%v", checks)
	}
	if m.deviceLabel(context.Background()) != "cpu" {
		t.Fatalf("deviceLabel=%q", m.deviceLabel(context.Background()))
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
	got := m.vllmCommandArguments(model{ID: "internal", Path: "/models/model", LaunchArguments: arguments}, "gpu")
	joined := strings.Join(got, " ")
	for _, expected := range []string{"serve /models/model", "--quantization modelopt_fp4", "--max-model-len 262144", "--gpu-memory-utilization 0.8", "--cudagraph-capture-sizes 4", "--no-enable-flashinfer-autotune", "--enable-auto-tool-choice", "--served-model-name qwen3.6", "--tool-call-parser qwen3_coder", "--host 0.0.0.0", "--port 1"} {
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

func TestVLLMCommandArgumentsDefaultGPUMemoryUtilization(t *testing.T) {
	m := testManager(t)
	got := m.vllmCommandArguments(model{ID: "Qwen/Qwen3-0.6B", Path: "/models/qwen", LaunchArguments: []string{"--max-model-len", "8192"}}, "gpu")
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--gpu-memory-utilization 0.8") {
		t.Fatalf("GPU mode should default to 0.8 utilization, got %q", joined)
	}
	cpu := m.vllmCommandArguments(model{ID: "x", Path: "/models/x", LaunchArguments: nil}, "cpu")
	if strings.Contains(strings.Join(cpu, " "), "--gpu-memory-utilization") {
		t.Fatalf("CPU mode must not set gpu-memory-utilization: %v", cpu)
	}
}

func TestGpuExtendedResourceRequestDefaultOff(t *testing.T) {
	t.Setenv("INFERENCE_GPU_RESOURCE_REQUEST", "")
	if gpuExtendedResourceRequested() {
		t.Fatal("default must not request nvidia.com/gpu without a device plugin")
	}
	t.Setenv("INFERENCE_GPU_RESOURCE_REQUEST", "true")
	if !gpuExtendedResourceRequested() {
		t.Fatal("expected opt-in true")
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
		if r.URL.Path == "/api/show" {
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"completion", "tools"}})
			return
		}
		if r.URL.Path == "/api/pull" {
			_, _ = w.Write([]byte("{\"status\":\"success\"}\n"))
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
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "tiny:latest") || !strings.Contains(w.Body.String(), `"codexCompatible":true`) {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	joined := strings.Join(calls, ",")
	if !strings.Contains(joined, "POST /api/pull") || !strings.Contains(joined, "GET /api/tags") || !strings.Contains(joined, "POST /api/show") {
		t.Fatalf("engine calls=%v", calls)
	}
}

func TestOllamaModelWithoutToolsIsChatOnly(t *testing.T) {
	if got := ollamaCapabilities([]string{"completion", "thinking"}, "{{ .Prompt }}"); got.CodexCompatible || got.ToolCalling || !reflect.DeepEqual(got.Experiences, []string{"chat"}) {
		t.Fatalf("unexpected Ollama chat capabilities: %#v", got)
	}
}

func TestOllamaInventoryServesPersistedSnapshotWithoutRuntimeCall(t *testing.T) {
	called := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()
	m := testManager(t)
	m.engine = "ollama"
	m.backend, _ = url.Parse(backend.URL)
	m.ollama = ollamaInventory{UpdatedAt: time.Now().UTC(), Models: []model{{
		ID:           "qwen2.5-coder:1.5b",
		Capabilities: modelCapabilities{Experiences: []string{"chat", "coding-agent"}, ToolCalling: true, ResponsesCompatible: true, CodexCompatible: true, Verification: "template-reported"},
	}}}
	w := httptest.NewRecorder()
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "qwen2.5-coder:1.5b") || !strings.Contains(w.Body.String(), `"codexCompatible":true`) {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("cached inventory read called the Ollama runtime")
	}
}

func TestOllamaInventoryPersistsAcrossManagerRestart(t *testing.T) {
	m := testManager(t)
	m.engine = "ollama"
	m.ollama = ollamaInventory{UpdatedAt: time.Now().UTC(), Models: []model{{ID: "qwen2.5-coder:1.5b", Capabilities: toolCapableOllamaModel("template-reported")}}}
	m.mu.Lock()
	err := m.saveOllamaInventoryLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restarted := testManager(t)
	restarted.engine = "ollama"
	restarted.modelsDir = m.modelsDir
	if err := restarted.loadOllamaInventory(); err != nil {
		t.Fatal(err)
	}
	got := restarted.ollamaInventorySnapshot()
	if len(got) != 1 || got[0].ID != "qwen2.5-coder:1.5b" || !got[0].Capabilities.CodexCompatible {
		t.Fatalf("restarted inventory = %#v", got)
	}
}

func TestBootstrapModelMetadataPopulatesLocalOllamaInventory(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen2.5-coder:7b"}}})
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"completion", "tools"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	m := testManager(t)
	m.engine = "ollama"
	m.backend, _ = url.Parse(backend.URL)
	m.bootstrapModelMetadata(context.Background())
	inventory := m.ollamaInventorySnapshot()
	if len(inventory) != 1 || !inventory[0].Capabilities.CodexCompatible {
		t.Fatalf("startup Ollama inventory = %#v", inventory)
	}
}

func TestOllamaShowFailurePreservesKnownCapabilityWithoutGuessingNewModel(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "known:1b"}, {"name": "known-empty:1b"}, {"name": "new:1b"}}})
		case "/api/show":
			var request struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.Name == "known:1b" {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	m := testManager(t)
	m.engine = "ollama"
	m.backend, _ = url.Parse(backend.URL)
	m.ollama.Models = []model{
		{ID: "known:1b", Capabilities: toolCapableOllamaModel("runtime-reported")},
		{ID: "known-empty:1b", Capabilities: toolCapableOllamaModel("template-reported")},
	}
	m.refreshOllamaInventory(context.Background())
	got := m.ollamaInventorySnapshot()
	if len(got) != 3 || got[0].ID != "known-empty:1b" || !got[0].Capabilities.CodexCompatible || got[1].ID != "known:1b" || !got[1].Capabilities.CodexCompatible || got[2].ID != "new:1b" || got[2].Capabilities.Verification != "unverified" {
		t.Fatalf("failed /api/show changed known support or guessed new support: %+v", got)
	}
}

func TestOllamaToolTemplateIsCodingAgentWhenRuntimeOmitsCapabilities(t *testing.T) {
	got := ollamaCapabilities(nil, "{{- if .Tools }}tools{{ end }}")
	if !got.CodexCompatible || got.Verification != "template-reported" {
		t.Fatalf("unexpected Ollama template capabilities: %#v", got)
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
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil))
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

func TestConfiguredToolCallingModelIsAdvertisedAsCodingAgent(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	m.reg.Models["qwen-coder"] = model{
		ID: "qwen-coder",
		LaunchArguments: []string{
			"--enable-auto-tool-choice",
			"--tool-call-parser", "qwen3_coder",
		},
	}
	w := httptest.NewRecorder()
	m.listModels(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Data []model `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 || !response.Data[0].Capabilities.CodexCompatible {
		t.Fatalf("expected Codex-compatible model, got %#v", response.Data)
	}
	if got := response.Data[0].Capabilities.Experiences; !reflect.DeepEqual(got, []string{"chat", "coding-agent"}) {
		t.Fatalf("experiences = %#v", got)
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
	m.engine = "vllm"
	m.gpuProbe = func(context.Context) bool { return true }
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer backend.Close()
	m.backend, _ = url.Parse(backend.URL)
	fake := &fakeEngine{}
	m.engineOrch = fake
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi")
	t.Setenv("INFERENCE_GPU_ENABLED", "true")
	modelDir := filepath.Join(m.modelsDir, "tiny")
	if err := os.MkdirAll(modelDir, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "weights.bin"), make([]byte, 1024), 0o660); err != nil {
		t.Fatal(err)
	}
	item := model{ID: "test/tiny", Path: modelDir}
	m.reg.Models[item.ID] = item
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/models/load", strings.NewReader(`{"modelId":"test/tiny"}`))
	w := httptest.NewRecorder()
	m.loadModel(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("load status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"state":"loading"`) {
		t.Fatalf("expected async loading accept: %s", w.Body.String())
	}
	waitLoadState(t, m, "ready")
	spec, ok := fake.lastApplied()
	if !ok {
		t.Fatal("expected engine Deployment apply")
	}
	if len(spec.Command) == 0 || spec.Command[0] != "vllm" {
		t.Fatalf("command=%v", spec.Command)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "serve "+modelDir) || !strings.Contains(joined, "--host 0.0.0.0") {
		t.Fatalf("args=%v", spec.Args)
	}
	if fake.deleted < 1 {
		t.Fatal("expected previous engine delete before apply")
	}
	w = httptest.NewRecorder()
	m.handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/v1/models/load/progress", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"ready"`) {
		t.Fatalf("progress status %d: %s", w.Code, w.Body.String())
	}
	if got := m.instanceSummaries(); len(got) != 1 || got[0].ID != defaultInstanceID || len(got[0].Models) != 1 || got[0].Models[0] != item.ID {
		t.Fatalf("default instance after Load = %+v", got)
	}
}

func TestLoadAlreadyReadyIsIdempotent(t *testing.T) {
	m := testManager(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer backend.Close()
	m.backend, _ = url.Parse(backend.URL)
	fake := m.engineOrch.(*fakeEngine)
	fake.exists = true
	fake.ready = true
	m.reg.Models["test/tiny"] = model{ID: "test/tiny", Path: filepath.Join(m.modelsDir, "tiny")}
	m.active = "test/tiny"
	m.finishLoadProgress("ready", "test/tiny", "Model is ready for use")
	before := len(fake.applied)
	w := httptest.NewRecorder()
	m.loadModel(w, httptest.NewRequest(http.MethodPost, "/internal/v1/models/load", strings.NewReader(`{"modelId":"test/tiny"}`)))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"state":"ready"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if len(fake.applied) != before {
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
