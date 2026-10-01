package main

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

func TestEngineDeploymentRequiresVerifiedInferenceNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	engine := &k8sEngine{client: client, namespace: "inference", deployment: "engine-a", service: "engine-a", image: "registry.local/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nodeName: "gpu-1", requireInferenceNode: true, modelsClaim: "models", engine: "vllm"}
	if err := engine.ApplyEngine(context.Background(), enginePodSpec{ModelID: "org/model", Command: []string{"serve"}, MemoryLimit: resource.MustParse("2Gi"), MemoryReq: resource.MustParse("1Gi"), CPULimit: resource.MustParse("1"), CPURequest: resource.MustParse("1")}); err != nil {
		t.Fatal(err)
	}
	deployment, err := client.AppsV1().Deployments("inference").Get(context.Background(), "engine-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Template.Spec.NodeName != "gpu-1" || deployment.Spec.Template.Spec.NodeSelector["zon.io/inference-node"] != "true" {
		t.Fatalf("engine placement = %+v", deployment.Spec.Template.Spec)
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
	t.Setenv("INFERENCE_ENGINE_CPU_LIMIT", "")
	t.Setenv("INFERENCE_ENGINE_CPU_REQUEST", "")
	t.Setenv("INFERENCE_GPU_ENABLED", "false")
	serve, err := planServe(serveWindowInput{
		Engine: "vllm", Mode: "cpu", ModelEstimateBytes: 4 << 30, AvailableBytes: 32 << 30,
		AvailableCPUs: 8, ModelContextLimit: 8192, KVBytesPerToken: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := engineResourceSpecFromPlan(serve)
	if err != nil {
		t.Fatal(err)
	}
	if spec.MemoryLimit.Value() != int64(serve.RequiredBytes) {
		t.Fatalf("limit=%d required=%d", spec.MemoryLimit.Value(), serve.RequiredBytes)
	}
	if spec.CPULimit.Cmp(resource.MustParse("6")) != 0 { // 75% of 8
		t.Fatalf("cpu limit=%s", spec.CPULimit.String())
	}
	if spec.OMPThreads != 6 {
		t.Fatalf("omp=%d", spec.OMPThreads)
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
