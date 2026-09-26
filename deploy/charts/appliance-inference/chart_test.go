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
		"kind: ServiceAccount\nmetadata:\n  name: inference-gateway-manager",
		"kind: Role\nmetadata:\n  name: inference-gateway-manager",
		"kind: RoleBinding\nmetadata:\n  name: inference-gateway-manager",
		"runAsUser: 10006",
		"name: inference-manager",
		"name: INFERENCE_BACKEND_URL\n              value: \"http://inference-engine.inference.svc.cluster.local:8001\"",
		"name: INFERENCE_ENGINE\n              value: \"ollama\"",
		"name: INFERENCE_RUNTIME_IMAGE",
		"kind: PersistentVolume",
		"name: inference-gateway-models",
		"path: \"/data/zon/inference/models\"",
		"kind: PersistentVolumeClaim",
		"claimName: models",
		"{protocol: TCP, port: 443}",
		"app.kubernetes.io/name: inference-engine",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if strings.Contains(out, "name: INFERENCE_ENGINE_MAX_MEMORY") {
		t.Error("default chart must not set a hard-coded engine maxMemory ceiling")
	}
	if strings.Contains(out, "name: INFERENCE_ENGINE_CPU_LIMIT") || strings.Contains(out, "name: INFERENCE_ENGINE_CPU_REQUEST") {
		t.Error("default chart must not pin engine CPU; manager plans from host capacity")
	}
	if strings.Contains(out, "command: [\"ollama\", \"serve\"]") {
		t.Error("ollama serve belongs on the on-demand engine Deployment, not the manager chart")
	}
	if strings.Contains(out, "mountPath: /control") || strings.Contains(out, "run.sh") {
		t.Error("process-supervisor /control launcher must be removed")
	}
	if strings.Contains(out, "hostNetwork: true") {
		t.Error("inference chart must not enable hostNetwork")
	}
	if strings.Contains(out, "kind: Namespace") {
		t.Error("default render must not own Namespace; zonctl EnsureNamespace creates it")
	}
	if strings.Contains(out, "volumes:\n        - name: models\n          hostPath:") {
		t.Error("models volume must not use pod-level hostPath under Restricted PSA")
	}
	if !strings.Contains(out, "volumes:\n        - name: models\n          persistentVolumeClaim:\n            claimName: models") {
		t.Error("models volume must mount the models PVC")
	}
	// Manager-only Deployment: exactly one container in the gateway pod template.
	if strings.Count(out, "name: inference-manager") < 1 {
		t.Error("manager container missing")
	}
}

func TestGPUEnvPassedToManager(t *testing.T) {
	out := render(t, "--set", "runtime.engine=vllm", "--set", "gpu.enabled=true")
	for _, want := range []string{
		"name: INFERENCE_GPU_ENABLED\n              value: \"true\"",
		"name: INFERENCE_GPU_RUNTIME_CLASS\n              value: \"nvidia\"",
		"name: INFERENCE_GPU_VISIBLE_DEVICES\n              value: \"all\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("GPU manager env missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "runtimeClassName: \"nvidia\"") {
		t.Fatal("steady-state manager Deployment must not set GPU runtimeClassName")
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

func TestEngineEgressAllowsOllamaPullDuringConnectedWindow(t *testing.T) {
	out := render(t)
	const marker = "name: inference-gateway-engine\n  namespace: inference\nspec:\n  podSelector:\n    matchLabels:\n      app.kubernetes.io/name: inference-engine"
	start := strings.Index(out, marker)
	if start < 0 {
		t.Fatalf("engine NetworkPolicy not found: %s", out)
	}
	section := out[start:]
	if end := strings.Index(section, "\n---\n"); end > 0 {
		section = section[:end]
	}
	for _, want := range []string{
		"protocol: UDP",
		"port: 53",
		"port: 443",
		"kubernetes.io/metadata.name: kube-system",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("engine NetworkPolicy missing %q during connected window:\n%s", want, section)
		}
	}
	if strings.Contains(section, "egress: []") {
		t.Fatalf("engine NetworkPolicy must not deny all egress during connected window:\n%s", section)
	}
}

func TestImageDigestWins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	managerDigest := "sha256:" + strings.Repeat("d", 64)
	out := render(t, "--set", "image.digest="+digest, "--set", "managerImage.digest="+managerDigest)
	if !strings.Contains(out, "INFERENCE_RUNTIME_IMAGE\n              value: \"registry.local/inference-runtime@"+digest+"\"") {
		t.Fatalf("digest-pinned runtime image env not rendered")
	}
	if !strings.Contains(out, "image: registry.local/inference-manager@"+managerDigest) {
		t.Fatalf("digest-pinned manager image not rendered")
	}
}

func TestOpenWebUIWorkloadsAreExplicitlyOptIn(t *testing.T) {
	base := render(t)
	if strings.Contains(base, "name: inference-gateway-open-webui") {
		t.Fatal("Open WebUI must not render unless explicitly enabled")
	}
	webUIDigest := "sha256:" + strings.Repeat("e", 64)
	gatewayDigest := "sha256:" + strings.Repeat("f", 64)
	out := render(t,
		"--set", "openWebUI.enabled=true",
		"--set", "openWebUI.image.digest="+webUIDigest,
		"--set", "openWebUI.gatewayImage.digest="+gatewayDigest,
	)
	for _, want := range []string{
		"name: inference-gateway-open-webui",
		"name: inference-gateway-open-webui-gateway",
		"image: registry.local/open-webui@" + webUIDigest,
		"image: registry.local/open-webui-gateway@" + gatewayDigest,
		"mountPath: /app/backend/data",
		"WEBUI_AUTH_TRUSTED_EMAIL",
		"value: \"true\"",
		"ENABLE_SIGNUP",
		"ENABLE_API_KEYS",
		"ENABLE_PASSWORD_AUTH",
		"ENABLE_LOGIN_FORM",
		"ENABLE_INITIAL_ADMIN_SIGNUP",
		"ENABLE_PERSISTENT_CONFIG",
		"ENABLE_OLLAMA_API",
		"BYPASS_MODEL_ACCESS_CONTROL",
		"OPENAI_API_BASE_URL",
		"http://inference-gateway.inference.svc.cluster.local:8080/v1",
		"CONTROL_PLANE_URL",
		"http://controlplane.ace-system.svc.cluster.local:8080",
		"USER_PERMISSIONS_WORKSPACE_MODELS_ACCESS",
		"WEBUI_SECRET_KEY",
		"app.kubernetes.io/component: open-webui-gateway",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Open WebUI render missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "appliance-control-plane.ace-system") {
		t.Fatal("gateway must call controlplane Service DNS, not the chart/image name")
	}
	// Open WebUI must resolve inference-gateway Service DNS to list models.
	openWebUINP := "name: inference-gateway-open-webui\n"
	idx := strings.Index(out, openWebUINP)
	if idx < 0 {
		t.Fatal("open-webui NetworkPolicy missing")
	}
	npSlice := out[idx:]
	if end := strings.Index(npSlice[1:], "\n---\n"); end > 0 {
		npSlice = npSlice[:end+1]
	}
	if !strings.Contains(npSlice, "kubernetes.io/metadata.name: kube-system") || !strings.Contains(npSlice, "port: 53") {
		t.Fatal("open-webui NetworkPolicy must allow DNS to kube-system so OPENAI_API_BASE_URL resolves")
	}
	if !strings.Contains(npSlice, "port: 8080") || !strings.Contains(npSlice, "port: 11434") {
		t.Fatal("open-webui NetworkPolicy must allow egress to inference-gateway on service.port and targetPort")
	}
	if !strings.Contains(out, "BYPASS_MODEL_ACCESS_CONTROL\n              value: \"true\"") {
		t.Fatal("Open WebUI must bypass native model ACLs so role=user can see the served OpenAI model")
	}
	if strings.Contains(out, "kind: Ingress") {
		t.Fatal("the session bridge must own the future public route; chart must not expose Open WebUI directly")
	}
	for _, forbidden := range []string{
		"ENABLE_PASSWORD_AUTH\n              value: \"true\"",
		"ENABLE_OLLAMA_API\n              value: \"true\"",
		"ENABLE_API_KEYS\n              value: \"true\"",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("Open WebUI lockdown regressed: %q", forbidden)
		}
	}
}

func TestVLLMRuntimeContractRenders(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	out := render(t, "--set", "runtime.engine=vllm", "--set", "gpu.enabled=true")
	for _, want := range []string{
		"name: INFERENCE_ENGINE", "value: \"vllm\"",
		"name: INFERENCE_GPU_ENABLED", "value: \"true\"",
		"name: VLLM_CPU_KVCACHE_SPACE",
		"name: INFERENCE_ARCH_FILE", "value: \"/models/.zon/vllm-architectures.json\"",
		"kind: Job", "list_vllm_archs.py", "vllm-architectures.json",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("vLLM runtime contract missing %q: %s", want, out)
		}
	}
	for _, forbidden := range []string{
		"name: INFERENCE_MODE\n", "INFERENCE_SUPPORTED_MODES", "supportedModes",
		"mountPath: /control", "engine-launcher", "run.sh",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("vLLM chart must not render %q", forbidden)
		}
	}
}
