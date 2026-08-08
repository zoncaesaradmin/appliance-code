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
		"--set", "image.digest=sha256:" + strings.Repeat("a", 64),
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
		"name: HOME",
		"value: \"/home/ollama\"",
		"name: OLLAMA_MODELS",
		"value: \"/models\"",
		"kind: PersistentVolume",
		"name: inference-gateway-models",
		"path: \"/data/zon/inference/models\"",
		"kind: PersistentVolumeClaim",
		"claimName: models",
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

func TestImageDigestWins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	out := render(t, "--set", "image.digest="+digest)
	if !strings.Contains(out, "image: registry.local/inference-runtime@"+digest) {
		t.Fatalf("digest-pinned image not rendered")
	}
}
