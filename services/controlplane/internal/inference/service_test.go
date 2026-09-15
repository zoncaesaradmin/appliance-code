package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogReadUsesRuntimeCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/internal/v1/models/catalog" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"engine":"ollama","stale":true,"items":[]}`))
	}))
	defer server.Close()
	s, err := New(Config{BaseURL: server.URL, Engine: "ollama"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Catalog(context.Background())
	if err != nil || !json.Valid(result) {
		t.Fatalf("catalog=%s error=%v", result, err)
	}
}

func TestOllamaModelLifecycle(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "tiny:latest", "object": "model"}}})
		case "/internal/v1/models/imports", "/internal/v1/models/load", "/internal/v1/models/delete":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service, err := New(Config{BaseURL: server.URL, Package: "std-llm-amd64", Engine: "ollama", Architecture: "amd64", SupportedModes: []string{"cpu"}, RequestedMode: "cpu"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	models, err := service.ListModels(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "tiny:latest" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if err := service.Import(t.Context(), ImportRequest{ModelID: "tiny:latest", Source: "tiny:latest"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Load(t.Context(), "tiny:latest"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), "tiny:latest"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestAutoModeUsesRuntimeDetectionForVLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/runtime/capabilities" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"availableModes": []string{"cpu", "cuda"}, "activeMode": "cuda"})
	}))
	defer server.Close()
	service, err := New(Config{BaseURL: server.URL, Package: "acc-llm-arm64", Engine: "vllm", Architecture: "arm64", SupportedModes: []string{"cpu", "cuda"}, RequestedMode: "auto"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got := service.Capabilities(t.Context()).ActiveMode; got != "cuda" {
		t.Fatalf("activeMode=%q", got)
	}
}

func TestExplicitVLLMModeMustMatchRuntimeDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"availableModes": []string{"cpu", "cuda"}, "activeMode": "cpu"})
	}))
	defer server.Close()
	service, err := New(Config{BaseURL: server.URL, Package: "acc-llm-arm64", Engine: "vllm", Architecture: "arm64", SupportedModes: []string{"cpu", "cuda"}, RequestedMode: "cuda"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got := service.Capabilities(t.Context()).ActiveMode; got != "" {
		t.Fatalf("activeMode=%q, want unconfirmed", got)
	}
}

func TestEmptyRequestedModeDefaultsToAuto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"availableModes": []string{"cpu", "cuda"}, "activeMode": "cpu"})
	}))
	defer server.Close()
	service, err := New(Config{BaseURL: server.URL, Package: "acc-llm-arm64", Engine: "vllm", Architecture: "arm64", SupportedModes: []string{"cpu", "cuda"}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := service.Capabilities(t.Context())
	if capabilities.RequestedMode != "auto" || capabilities.ActiveMode != "cpu" {
		t.Fatalf("capabilities=%+v", capabilities)
	}
}

func TestImportRejectsUnsafeOrUnverifiableInput(t *testing.T) {
	service, err := New(Config{BaseURL: "http://inference.invalid", Engine: "ollama"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []ImportRequest{
		{ModelID: "model", Source: "https://example.invalid/model"},
		{ModelID: "alias", Source: "model:latest"},
		{ModelID: "model:latest", Source: "model:latest", Digest: "sha256:nope"},
		{ModelID: "model:latest", Source: "model:latest", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		if err := service.Import(t.Context(), request); err == nil {
			t.Fatalf("request unexpectedly accepted: %+v", request)
		}
	}
}

func TestVLLMLaunchArgumentsAreValidatedBeforeRuntimeCall(t *testing.T) {
	service, err := New(Config{BaseURL: "http://inference.invalid", Engine: "vllm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Import(t.Context(), ImportRequest{ModelID: "model", Source: "org/model", LaunchArguments: []string{"--host", "0.0.0.0"}}); err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("manager-owned argument error=%v", err)
	}
}
