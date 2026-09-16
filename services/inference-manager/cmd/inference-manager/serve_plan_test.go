package main

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPlanServeCPUCapsToPrefillBudget(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_CPU_LIMIT", "")
	t.Setenv("INFERENCE_ENGINE_CPU_REQUEST", "")
	// Qwen2.5-3B-class: card 32768, ~36KiB/token, plenty of host RAM → CPU mode
	// prefill cap (4 × 2048) must win.
	plan, err := planServe(serveWindowInput{
		Engine:             "vllm",
		Mode:               "cpu",
		ModelEstimateBytes: 10 << 30,
		AvailableBytes:     24 << 30,
		AvailableCPUs:      16,
		ModelContextLimit:  32768,
		KVBytesPerToken:    36864,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaxModelLen != 8192 {
		t.Fatalf("MaxModelLen=%d want 8192", plan.MaxModelLen)
	}
	if plan.CPUCores != 12 { // 75% of 16
		t.Fatalf("CPUCores=%d want 12", plan.CPUCores)
	}
	if plan.CPULimit.Cmp(resource.MustParse("12")) != 0 {
		t.Fatalf("CPULimit=%s", plan.CPULimit.String())
	}
	if plan.KVBytes != 8192*36864 {
		t.Fatalf("KVBytes=%d", plan.KVBytes)
	}
	wantRequired := plan.ModelEstimateBytes + plan.ShmBytes + plan.PodMarginBytes + plan.KVBytes
	if plan.RequiredBytes != wantRequired {
		t.Fatalf("RequiredBytes=%d want %d", plan.RequiredBytes, wantRequired)
	}
}

func TestPlanServeCUDAAllowsLargerWindow(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_CPU_LIMIT", "")
	plan, err := planServe(serveWindowInput{
		Engine:             "vllm",
		Mode:               "cuda",
		ModelEstimateBytes: 10 << 30,
		AvailableBytes:     80 << 30,
		AvailableCPUs:      16,
		ModelContextLimit:  32768,
		KVBytesPerToken:    36864,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaxModelLen != 32768 {
		t.Fatalf("MaxModelLen=%d want card 32768", plan.MaxModelLen)
	}
	if plan.CPUCores != 4 {
		t.Fatalf("CUDA CPUCores=%d want 4", plan.CPUCores)
	}
}

func TestPlanServeMemoryShrinksWindow(t *testing.T) {
	plan, err := planServe(serveWindowInput{
		Engine:             "vllm",
		Mode:               "cuda",
		ModelEstimateBytes: 4 << 30,
		AvailableBytes:     20 << 30,
		ModelContextLimit:  32768,
		KVBytesPerToken:    1 << 20, // 1 MiB/token forces a memory-bound window
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaxModelLen == 0 || plan.MaxModelLen >= 32768 {
		t.Fatalf("expected memory-shrunk window, got %d", plan.MaxModelLen)
	}
	if plan.RequiredBytes > 20<<30 {
		t.Fatalf("required %d exceeds budget", plan.RequiredBytes)
	}
}

func TestModelArchKVBytesPerToken(t *testing.T) {
	dir := t.TempDir()
	// Qwen2-style GQA: 36 layers, 2 kv heads, hidden 2048 / 16 heads = 128 dim
	cfg := `{"max_position_embeddings":32768,"num_hidden_layers":36,"num_key_value_heads":2,"num_attention_heads":16,"hidden_size":2048}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	arch := modelArchFromModelDir(dir)
	if arch.MaxPosition != 32768 || arch.Layers != 36 || arch.KVHeads != 2 || arch.HeadDim != 128 {
		t.Fatalf("arch=%+v", arch)
	}
	if got := arch.kvBytesPerToken(); got != 2*36*2*128*2 {
		t.Fatalf("kv/token=%d", got)
	}
}

func TestApplyServeWindowReplacesOversizedCardWindow(t *testing.T) {
	got := applyServeWindowMaxModelLen([]string{"--max-model-len", "32768"}, 8192, 32768)
	if len(got) != 2 || got[1] != "8192" {
		t.Fatalf("got %v", got)
	}
	kept := applyServeWindowMaxModelLen([]string{"--max-model-len", "4096"}, 8192, 32768)
	if kept[1] != "4096" {
		t.Fatalf("explicit lower window should remain, got %v", kept)
	}
}

func TestModePrefillContextCap(t *testing.T) {
	if modePrefillContextCap("cpu") != 8192 {
		t.Fatalf("cpu cap=%d", modePrefillContextCap("cpu"))
	}
	if modePrefillContextCap("cuda") != 131072 {
		t.Fatalf("cuda cap=%d", modePrefillContextCap("cuda"))
	}
}
