package main

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPlanModelMemoryIncludesShmAndMargin(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi")
	plan, err := planModelMemory("vllm", 4<<30, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(4<<30) + uint64(4<<30) + podMarginBytes
	if plan.RequiredBytes != want {
		t.Fatalf("required=%d want=%d", plan.RequiredBytes, want)
	}
}

func TestPlanModelMemoryRejectsAboveHostBudget(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi")
	_, err := planModelMemory("vllm", 10<<30, 12<<30)
	if err == nil {
		t.Fatal("expected host budget rejection")
	}
}

func TestPlanModelMemoryOptionalPackageMax(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "8Gi")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "0")
	_, err := planModelMemory("ollama", 10<<30, 64<<30)
	if err == nil {
		t.Fatal("expected package max rejection")
	}
}

func TestEngineResourceSpecMatchesPlan(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	t.Setenv("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi")
	t.Setenv("INFERENCE_ENGINE_CPU_LIMIT", "4")
	t.Setenv("INFERENCE_ENGINE_CPU_REQUEST", "100m")
	t.Setenv("INFERENCE_GPU_ENABLED", "false")
	spec, err := engineResourceSpec("vllm", 4<<30, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planModelMemory("vllm", 4<<30, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	if spec.MemoryLimit.Value() != int64(plan.RequiredBytes) {
		t.Fatalf("limit=%d required=%d", spec.MemoryLimit.Value(), plan.RequiredBytes)
	}
	if spec.MemoryReq.Cmp(resource.MustParse("512Mi")) < 0 {
		t.Fatalf("request too small: %s", spec.MemoryReq.String())
	}
}

func TestOllamaPlanHasNoShm(t *testing.T) {
	t.Setenv("INFERENCE_ENGINE_MAX_MEMORY", "")
	plan, err := planModelMemory("ollama", 2<<30, 16<<30)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ShmBytes != 0 {
		t.Fatalf("shm=%d", plan.ShmBytes)
	}
	if plan.RequiredBytes != (2<<30)+podMarginBytes {
		t.Fatalf("required=%d", plan.RequiredBytes)
	}
}
