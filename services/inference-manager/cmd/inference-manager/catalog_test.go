package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogRetainsGoodSnapshotOfflineAndAcrossRestart(t *testing.T) {
	m := testManager(t)
	m.engine = "ollama"
	c := newModelCatalog(m)
	c.discover = func(context.Context) ([]catalogEntry, error) {
		return []catalogEntry{{ID: "test:small", Source: "test:small", DownloadBytes: 1, MemoryBytes: 2}}, nil
	}
	c.refresh(context.Background())
	first := c.state.LastSuccess
	if first.IsZero() {
		t.Fatal("successful refresh was not recorded")
	}
	c.discover = func(context.Context) ([]catalogEntry, error) { return nil, errors.New("offline") }
	c.refresh(context.Background())
	restarted := newModelCatalog(m)
	restarted.budget = func(context.Context) (uint64, uint64) {
		plan, _ := planModelMemory(m.engine, 2, 1<<30)
		return plan.RequiredBytes + 1, 10
	}
	snapshot := restarted.snapshot(context.Background())
	if len(snapshot.Items) != 1 || !snapshot.Items[0].Eligible || snapshot.LastError == "" || !snapshot.Stale || !snapshot.LastSuccess.Equal(first) {
		t.Fatalf("lost offline snapshot: %+v", snapshot)
	}
	if snapshot.LastAttempt.IsZero() {
		t.Fatal("refresh schedule not persisted")
	}
	called := false
	restarted.discover = func(context.Context) ([]catalogEntry, error) { called = true; return nil, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	restarted.run(ctx)
	if called {
		t.Fatal("fresh persisted schedule triggered another discovery")
	}
}

func TestOllamaCatalogIsInvalidatedWhenRuntimeVersionChanges(t *testing.T) {
	t.Setenv("INFERENCE_RUNTIME_VERSION", "0.9.0")
	m := testManager(t)
	m.engine = "ollama"
	c := newModelCatalog(m)
	c.discover = func(context.Context) ([]catalogEntry, error) {
		return []catalogEntry{{ID: "qwen3:1.7b", Source: "qwen3:1.7b", DownloadBytes: 1, MemoryBytes: 2}}, nil
	}
	c.refresh(context.Background())
	if c.state.RuntimeVersion != "0.9.0" || len(c.state.Items) != 1 {
		t.Fatalf("catalog state=%+v", c.state)
	}

	t.Setenv("INFERENCE_RUNTIME_VERSION", "0.9.1")
	restarted := newModelCatalog(m)
	if restarted.state.RuntimeVersion != "0.9.1" || len(restarted.state.Items) != 0 {
		t.Fatalf("catalog from prior runtime was reused: %+v", restarted.state)
	}
	if delay := restarted.nextRefreshDelay(); delay > 0 {
		t.Fatalf("changed runtime should refresh catalog immediately, delay=%s", delay)
	}
}

func TestEmptyFailedCatalogRetriesImmediatelyOnRestart(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	c := newModelCatalog(m)
	c.discover = func(context.Context) ([]catalogEntry, error) {
		return nil, errors.New("cannot determine installed vLLM model architectures: exit status 1")
	}
	c.refresh(context.Background())
	if len(c.state.Items) != 0 || c.state.LastError == "" || c.state.LastAttempt.IsZero() {
		t.Fatalf("failed empty catalog not recorded: %+v", c.state)
	}
	restarted := newModelCatalog(m)
	if d := restarted.nextRefreshDelay(); d != 0 {
		t.Fatalf("empty failed catalog should retry immediately, delay=%s", d)
	}
	called := false
	restarted.discover = func(context.Context) ([]catalogEntry, error) {
		called = true
		return []catalogEntry{{ID: "org/model", Source: "org/model@abc", DownloadBytes: 1, MemoryBytes: 2}}, nil
	}
	restarted.refresh(context.Background())
	if !called || len(restarted.state.Items) != 1 || restarted.state.LastError != "" {
		t.Fatalf("empty failed catalog did not recover: called=%v state=%+v", called, restarted.state)
	}
}

func TestFailedCatalogRetriesOnlyOncePerProcessStart(t *testing.T) {
	m := testManager(t)
	m.engine = "ollama"
	c := newModelCatalog(m)
	fail := func(context.Context) ([]catalogEntry, error) { return nil, errors.New("network disconnected") }
	c.discover = fail
	c.refresh(context.Background())
	if delay := c.nextRefreshDelay(); delay < 23*time.Hour {
		t.Fatalf("first failure must wait until next daily refresh: %s", delay)
	}
	restarted := newModelCatalog(m)
	if delay := restarted.nextRefreshDelay(); delay != 0 {
		t.Fatalf("failed empty catalog should retry on restart: %s", delay)
	}
	restarted.discover = fail
	restarted.refresh(context.Background())
	if delay := restarted.nextRefreshDelay(); delay < 23*time.Hour {
		t.Fatalf("restart retry failure must not spin: %s", delay)
	}
}

func TestCatalogSelectionRechecksCapacityWithoutNetwork(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	m.gpuProbe = func(context.Context) bool { return true }
	c := newModelCatalog(m)
	c.state.Items = []catalogEntry{{
		ID: "org/model", Source: "org/model@revision", DownloadBytes: 10, MemoryBytes: 20,
		ModelContextLimit: 4096, KVBytesPerToken: 1024,
	}}
	c.state.LastSuccess = time.Now()
	fit, err := planServe(serveWindowInput{
		Engine: "vllm", Mode: "gpu", ModelEstimateBytes: 20, AvailableBytes: 16 << 30,
		ModelContextLimit: 4096, KVBytesPerToken: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.budget = func(context.Context) (uint64, uint64) { return fit.RequiredBytes, 30 }
	if _, err := c.selection(context.Background(), "org/model"); err != nil {
		t.Fatal(err)
	}
	// Below reserved weights+shm+margin there is no workable context window.
	reserved := fit.ModelEstimateBytes + fit.ShmBytes + fit.PodMarginBytes
	c.budget = func(context.Context) (uint64, uint64) { return reserved, 30 }
	if _, err := c.selection(context.Background(), "org/model"); err == nil {
		t.Fatal("low-memory model accepted")
	}
	c.budget = func(context.Context) (uint64, uint64) { return 30, 19 }
	if _, err := c.selection(context.Background(), "org/model"); err == nil {
		t.Fatal("low-disk model accepted")
	}
	if _, err := c.selection(context.Background(), "unknown"); err == nil {
		t.Fatal("unknown model accepted")
	}
	m.catalog = c
	response := httptest.NewRecorder()
	m.handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/v1/models/catalog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog route: %d", response.Code)
	}
}

func TestEmptyRefreshDoesNotEraseCatalog(t *testing.T) {
	m := testManager(t)
	m.engine = "ollama"
	c := newModelCatalog(m)
	c.state.Items = []catalogEntry{{ID: "retained"}}
	c.discover = func(context.Context) ([]catalogEntry, error) { return nil, nil }
	c.refresh(context.Background())
	if len(c.state.Items) != 1 || c.state.LastError == "" {
		t.Fatal("empty discovery erased catalog")
	}
}

func TestInstalledVLLMArchitecturesUsesControlFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vllm-architectures.json")
	if err := os.WriteFile(path, []byte(`["LlamaForCausalLM","Qwen2ForCausalLM"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFERENCE_VLLM_ARCHITECTURES_FILE", path)
	supported, err := installedVLLMArchitectures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !supported["LlamaForCausalLM"] || !supported["Qwen2ForCausalLM"] {
		t.Fatalf("supported=%v", supported)
	}
}

func TestInstalledVLLMArchitecturesReadsRegistryWithoutImportingRuntime(t *testing.T) {
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
_EMBEDDING_MODELS = {"BertModel": ("bert", "BertModel")}
_VLLM_MODELS = {**_TEXT_GENERATION_MODELS, **_EMBEDDING_MODELS}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PYTHONPATH", root)
	supported, err := installedVLLMArchitectures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []string{"LlamaForCausalLM", "Qwen2ForCausalLM", "BertModel"} {
		if !supported[architecture] {
			t.Fatalf("missing %s: %v", architecture, supported)
		}
	}
}

func TestPythonOutputIncludesStderr(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for the vLLM architecture probe")
	}
	_, err := pythonOutput(context.Background(), 5*time.Second, "import sys; sys.stderr.write('probe exploded\\n'); raise SystemExit(1)", "-")
	if err == nil || !strings.Contains(err.Error(), "probe exploded") {
		t.Fatalf("stderr lost: %v", err)
	}
}

func TestOllamaLibraryParsing(t *testing.T) {
	page := `<a href="/library/example">Example</a><a href="/library/example:latest">latest</a><a href="/library/example:3b">small</a><a href="/library/example:3b">duplicate</a><a href="/library/example:cloud">cloud</a><a href="/library/example:7b">large</a><a href="https://evil.example/model">untrusted</a>`
	refs := libraryReferences(page, true, 8)
	if len(refs) != 2 || refs[0] != "example:3b" || refs[1] != "example:7b" {
		t.Fatalf("refs=%v", refs)
	}
	if len(libraryReferences(page, false, 24)) != 1 {
		t.Fatal("family discovery failed")
	}
	if len(libraryReferences(page, true, 1)) != 1 {
		t.Fatal("discovery limit not applied")
	}
}
