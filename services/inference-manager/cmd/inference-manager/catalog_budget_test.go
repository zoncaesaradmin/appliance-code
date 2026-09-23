package main

import "testing"

func TestOllamaMemoryEstimateUsesQuantizedLayerNotDoubleWeights(t *testing.T) {
	const weights = uint64(14_000_000_000)
	got := ollamaMemoryEstimate(weights)
	if got <= weights || got+(512<<20) >= 24<<30 {
		t.Fatalf("gpt-oss:20b estimate %d should include headroom and fit 24 GiB budget", got)
	}
	if gpuBudget := combineCatalogMemory(32<<30, 24<<30, true, "ollama"); got+(512<<20) >= gpuBudget {
		t.Fatalf("gpt-oss:20b estimate %d should fit a 24 GiB GPU budget of %d", got, gpuBudget)
	}
	if ollamaMemoryEstimate(0) != 0 {
		t.Fatal("unknown model layer must remain unknown")
	}
}

func TestCombineCatalogMemoryGPUProbeMissKeepsHost(t *testing.T) {
	const host uint64 = 120 << 30 // 120 GiB MemAvailable
	got := combineCatalogMemory(host, 0, true, "vllm")
	want := host / 4 * 3
	if got != want {
		t.Fatalf("got %d want host-derived %d (probe miss must not zero budget)", got, want)
	}
}

func TestCombineCatalogMemoryUsesSmallerGPU(t *testing.T) {
	const host uint64 = 120 << 30
	const gpuFree uint64 = 8 << 30
	got := combineCatalogMemory(host, gpuFree, true, "vllm")
	want := gpuFree / 5 * 4
	if got != want {
		t.Fatalf("got %d want GPU-derived %d", got, want)
	}
}

func TestCombineCatalogMemoryVLLMWithoutGPU(t *testing.T) {
	if got := combineCatalogMemory(120<<30, 0, false, "vllm"); got != 0 {
		t.Fatalf("accelerated without GPU must be 0, got %d", got)
	}
}

func TestCombineCatalogMemoryOllamaCPUUsesHost(t *testing.T) {
	const host uint64 = 16 << 30
	got := combineCatalogMemory(host, 0, false, "ollama")
	want := host / 4 * 3
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
