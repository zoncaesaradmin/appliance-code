//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogBudgetGPUProbeMissKeepsHostMemory(t *testing.T) {
	models := filepath.Join(t.TempDir(), "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &manager{engine: "vllm", modelsDir: models}
	m.gpuProbe = func(context.Context) bool { return true }

	prev := gpuFreeMemoryProbe
	gpuFreeMemoryProbe = func(context.Context) uint64 { return 0 }
	t.Cleanup(func() { gpuFreeMemoryProbe = prev })

	memory, disk := m.catalogBudget(context.Background())
	if disk == 0 {
		t.Fatal("expected free disk from temp models dir")
	}
	if memory == 0 {
		t.Fatal("GPU probe miss must not zero catalog memory when host MemAvailable is known")
	}
	want := combineCatalogMemory(hostMemAvailable(), 0, true, "vllm")
	if memory != want {
		t.Fatalf("memory=%d want %d", memory, want)
	}
}
