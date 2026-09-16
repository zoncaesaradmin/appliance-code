package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeVLLMRegistry(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the vLLM architecture probe")
	}
	root := t.TempDir()
	registry := filepath.Join(root, "vllm", "model_executor", "models", "registry.py")
	if err := os.MkdirAll(filepath.Dir(registry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vllm", "__init__.py"), []byte("raise RuntimeError('vllm import is forbidden')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, []byte(`_TEXT_GENERATION_MODELS = {
    "LlamaForCausalLM": ("llama", "LlamaForCausalLM"),
    "Qwen2ForCausalLM": ("qwen2", "Qwen2ForCausalLM"),
}
_VLLM_MODELS = {**_TEXT_GENERATION_MODELS}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PYTHONPATH", root)
	return root
}

func useCatalogFixture(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	upstreamTransport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(func() { upstreamTransport = nil })
	return server
}

func TestDiscoverVLLMUsesInstalledArchitecturesAndUpstreamMetadata(t *testing.T) {
	writeFakeVLLMRegistry(t)
	const (
		keepID   = "org/keep-model"
		keepSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		skipID   = "org/quantized-model"
		skipSHA  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		wrongID  = "org/wrong-arch"
		wrongSHA = "cccccccccccccccccccccccccccccccccccccccc"
	)
	server := useCatalogFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models") && r.URL.RawQuery != "" && !strings.Contains(r.URL.Path, "/revision/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": keepID, "sha": keepSHA, "gated": false, "private": false},
				{"id": skipID, "sha": skipSHA, "gated": false, "private": false},
				{"id": wrongID, "sha": wrongSHA, "gated": false, "private": false},
				{"id": "org/private", "sha": keepSHA, "gated": false, "private": true},
			})
		case r.URL.Path == "/"+keepID+"/resolve/"+keepSHA+"/config.json":
			http.Redirect(w, r, "/cdn/"+keepID+"/config.json", http.StatusFound)
		case r.URL.Path == "/cdn/"+keepID+"/config.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"architectures":           []string{"Qwen2ForCausalLM"},
				"max_position_embeddings": 4096,
			})
		case r.URL.Path == "/"+skipID+"/resolve/"+skipSHA+"/config.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"architectures":       []string{"Qwen2ForCausalLM"},
				"quantization_config": map[string]any{"bits": 4},
			})
		case r.URL.Path == "/"+wrongID+"/resolve/"+wrongSHA+"/config.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"architectures": []string{"TotallyUnknownForCausalLM"}})
		case r.URL.Path == "/api/models/"+keepID+"/revision/"+keepSHA:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"siblings": []map[string]any{
					{"rfilename": "model.safetensors", "size": 1_000_000},
					{"rfilename": "tokenizer.json", "size": 2_000},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("INFERENCE_CATALOG_HF_BASE", server.URL)

	m := testManager(t)
	m.engine = "vllm"
	entries, err := m.discoverVLLM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	got := entries[0]
	if got.ID != keepID || got.Source != keepID+"@"+keepSHA || got.DownloadBytes != 1_002_000 || got.MemoryBytes != 1_000_000*2+(4<<30) {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if len(got.LaunchArguments) != 2 || got.LaunchArguments[0] != "--max-model-len" || got.LaunchArguments[1] != "2048" {
		t.Fatalf("launch args: %v", got.LaunchArguments)
	}
}

func TestDiscoverVLLMClampsMaxModelLenToModelWindow(t *testing.T) {
	writeFakeVLLMRegistry(t)
	const (
		id  = "openai-community/gpt2"
		sha = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	)
	server := useCatalogFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models") && !strings.Contains(r.URL.Path, "/revision/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": id, "sha": sha, "gated": false, "private": false}})
		case strings.Contains(r.URL.Path, "/resolve/") && strings.HasSuffix(r.URL.Path, "/config.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"architectures":           []string{"GPT2LMHeadModel"},
				"max_position_embeddings": 1024,
			})
		case strings.Contains(r.URL.Path, "/revision/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"siblings": []map[string]any{{"rfilename": "model.safetensors", "size": 5000}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("INFERENCE_CATALOG_HF_BASE", server.URL)
	t.Setenv("INFERENCE_VLLM_ARCHITECTURES_FILE", filepath.Join(t.TempDir(), "missing.json"))
	// Prefer installed probe via fake registry; also publish architectures file.
	archFile := filepath.Join(t.TempDir(), "vllm-architectures.json")
	if err := os.WriteFile(archFile, []byte(`["GPT2LMHeadModel","Qwen2ForCausalLM","LlamaForCausalLM"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFERENCE_VLLM_ARCHITECTURES_FILE", archFile)

	m := testManager(t)
	m.engine = "vllm"
	entries, err := m.discoverVLLM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%+v", entries)
	}
	if got := entries[0].LaunchArguments; len(got) != 2 || got[0] != "--max-model-len" || got[1] != "1024" {
		t.Fatalf("launch args: %v", got)
	}
}

func TestDiscoverModelsRoutesVLLMThroughCatalogRefresh(t *testing.T) {
	writeFakeVLLMRegistry(t)
	const (
		id  = "org/catalog-model"
		sha = "dddddddddddddddddddddddddddddddddddddddd"
	)
	server := useCatalogFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models") && !strings.Contains(r.URL.Path, "/revision/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": id, "sha": sha, "gated": false, "private": false}})
		case strings.Contains(r.URL.Path, "/resolve/") && strings.HasSuffix(r.URL.Path, "/config.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"architectures": []string{"LlamaForCausalLM"}})
		case strings.Contains(r.URL.Path, "/revision/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"siblings": []map[string]any{{"rfilename": "weights.safetensors", "size": 5000}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("INFERENCE_CATALOG_HF_BASE", server.URL)

	m := testManager(t)
	m.engine = "vllm"
	c := newModelCatalog(m)
	c.budget = func(context.Context) (uint64, uint64) { return 1 << 40, 1 << 40 }
	c.refresh(context.Background())
	snapshot := c.snapshot(context.Background())
	if snapshot.LastError != "" || len(snapshot.Items) != 1 || snapshot.Items[0].ID != id || !snapshot.Items[0].Eligible {
		t.Fatalf("catalog refresh via discoverModels failed: %+v", snapshot)
	}
	response := httptest.NewRecorder()
	m.catalog = c
	m.handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/v1/models/catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog API status=%d body=%s", response.Code, response.Body.String())
	}
	var body catalogState
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != id || !body.Items[0].Eligible {
		t.Fatalf("catalog API body: %+v", body)
	}
}

func TestDiscoverOllamaUsesLibraryAndRegistryMetadata(t *testing.T) {
	server := useCatalogFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library":
			_, _ = w.Write([]byte(`<a href="/library/tiny">Tiny</a><a href="/library/skipme">Skip</a>`))
		case "/library/tiny/tags":
			_, _ = w.Write([]byte(`<a href="/library/tiny:1b">1b</a><a href="/library/tiny:latest">latest</a>`))
		case "/library/skipme/tags":
			_, _ = w.Write([]byte(`<a href="/library/skipme:cloud">cloud</a>`))
		case "/v2/library/tiny/manifests/1b":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]any{
					{"size": 1000, "mediaType": "application/vnd.ollama.image.model"},
					{"size": 50, "mediaType": "application/vnd.ollama.image.params"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("INFERENCE_CATALOG_OLLAMA_BASE", server.URL)
	t.Setenv("INFERENCE_CATALOG_OLLAMA_REGISTRY_BASE", server.URL)

	entries, err := discoverOllama(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "tiny:1b" || entries[0].DownloadBytes != 1050 || entries[0].MemoryBytes != 1000*2+(2<<30) {
		t.Fatalf("entries=%+v", entries)
	}
}
