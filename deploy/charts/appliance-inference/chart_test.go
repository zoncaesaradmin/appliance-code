package chart

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func chartDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve chart directory")
	}
	return filepath.Dir(file)
}

func render(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	command := append([]string{
		"template", "inference", chartDir(t), "--namespace", "inference",
		"--set", "image.digest=sha256:"+strings.Repeat("a", 64),
		"--set", "managerImage.digest=sha256:"+strings.Repeat("c", 64),
	}, args...)
	out, err := exec.Command("helm", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

func TestInferenceGatewayRender(t *testing.T) {
	out := render(t)
	for _, want := range []string{
		"kind: Service\nmetadata:\n  name: inference-gateway",
		"runAsUser: 10006",
		"name: inference-manager",
		"name: inference-engine",
		"command: [\"ollama\", \"serve\"]",
		"name: CUDA_VISIBLE_DEVICES\n              value: \"-1\"",
		"name: ROCR_VISIBLE_DEVICES\n              value: \"-1\"",
		"name: HOME",
		"value: \"/home/runtime\"",
		"name: OLLAMA_MODELS",
		"value: \"/models\"",
		"name: INFERENCE_ENGINE\n              value: \"ollama\"",
		"name: INFERENCE_BACKEND_URL\n              value: \"http://127.0.0.1:8001\"",
		"kind: PersistentVolume",
		"name: inference-gateway-models",
		"path: \"/data/zon/inference/models\"",
		"kind: PersistentVolumeClaim",
		"claimName: models",
		"{protocol: TCP, port: 443}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if strings.Contains(out, "hostNetwork: true") {
		t.Error("inference chart must not enable hostNetwork")
	}
	if strings.Contains(out, "kind: Namespace") {
		t.Error("default render must not own Namespace; zonctl EnsureNamespace creates it")
	}
	// Restricted PSA forbids pod-level hostPath; models must be PVC-backed.
	if strings.Contains(out, "volumes:\n        - name: models\n          hostPath:") {
		t.Error("models volume must not use pod-level hostPath under Restricted PSA")
	}
	if !strings.Contains(out, "volumes:\n        - name: models\n          persistentVolumeClaim:\n            claimName: models") {
		t.Error("models volume must mount the models PVC")
	}
}

func TestGPUContainerRuntimeMappingRenders(t *testing.T) {
	out := render(t, "--set", "runtime.engine=vllm", "--set", "runtime.supportedModes={cpu,cuda}", "--set", "gpu.enabled=true")
	for _, want := range []string{"runtimeClassName: \"nvidia\"", "name: NVIDIA_VISIBLE_DEVICES", "value: \"all\"", "name: NVIDIA_DRIVER_CAPABILITIES", "mountPath: /dev/shm"} {
		if !strings.Contains(out, want) {
			t.Fatalf("GPU runtime mapping missing %q: %s", want, out)
		}
	}
}

func TestModelsStorageClassPVCWhenHostPathDisabled(t *testing.T) {
	out := render(t, "--set", "persistence.hostPath.enabled=false")
	if strings.Contains(out, "kind: PersistentVolume\n") {
		t.Error("hostPath.enabled=false must not render a static PersistentVolume")
	}
	if !strings.Contains(out, "kind: PersistentVolumeClaim") {
		t.Error("hostPath.enabled=false must still render a PVC")
	}
	if strings.Contains(out, "path: \"/data/zon/inference/models\"") {
		t.Error("hostPath.enabled=false must not pin the host models path on a PV")
	}
	if !strings.Contains(out, "volumes:\n        - name: models\n          persistentVolumeClaim:\n            claimName: models") {
		t.Error("models volume must still mount the models PVC")
	}
}

func TestNamespaceCreateRendersRestrictedPSA(t *testing.T) {
	out := render(t, "--set", "namespace.create=true")
	for _, want := range []string{
		"kind: Namespace",
		"pod-security.kubernetes.io/enforce: restricted",
		"pod-security.kubernetes.io/audit: restricted",
		"pod-security.kubernetes.io/warn: restricted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestInferenceEgressCanBeClosedAfterConnectedWindow(t *testing.T) {
	out := render(t, "--set", "networkPolicy.allowDNSToKubeSystem=false", "--set", "networkPolicy.allowModelDownloadHTTPS=false")
	if !strings.Contains(out, "egress: []") {
		t.Fatalf("closed egress policy not rendered: %s", out)
	}
}

func TestImageDigestWins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	managerDigest := "sha256:" + strings.Repeat("d", 64)
	out := render(t, "--set", "image.digest="+digest, "--set", "managerImage.digest="+managerDigest)
	if !strings.Contains(out, "image: registry.local/inference-runtime@"+digest) {
		t.Fatalf("digest-pinned runtime image not rendered")
	}
	if !strings.Contains(out, "image: registry.local/inference-manager@"+managerDigest) {
		t.Fatalf("digest-pinned manager image not rendered")
	}
}

func TestVLLMRuntimeContractRenders(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out := render(t, "--set", "runtime.engine=vllm", "--set", "runtime.supportedModes={cpu,cuda}")
	for _, want := range []string{
		"name: INFERENCE_ENGINE", "value: \"vllm\"", "name: INFERENCE_MODE", "value: \"auto\"",
		"name: INFERENCE_SUPPORTED_MODES", "value: \"cpu,cuda\"", "name: VLLM_CPU_KVCACHE_SPACE",
		"mountPath: /dev/shm", "sizeLimit: 4Gi", "name: inference-gateway-engine-launcher",
		"mountPath: /control", "command: [\"/bin/sh\", \"/launcher/run.sh\"]",
		"list_vllm_archs.py", "vllm-architectures.json", "engine-exit.json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("vLLM runtime contract missing %q: %s", want, out)
		}
	}
}
