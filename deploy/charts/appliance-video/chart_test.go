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
		"template", "video", chartDir(t), "--namespace", "video",
		"--set", "image.digest=sha256:" + strings.Repeat("a", 64),
	}, args...)
	out, err := exec.Command("helm", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

func TestVideoGatewayRender(t *testing.T) {
	out := render(t)
	for _, want := range []string{
		"kind: Service\nmetadata:\n  name: video-gateway",
		"runAsUser: 10008",
		"name: JELLYFIN_DATA_DIR",
		"value: \"/config\"",
		"kind: PersistentVolume",
		"name: video-gateway-library",
		"path: \"/data/zon/video/library\"",
		"kind: PersistentVolumeClaim",
		"claimName: library",
		"mountPath: /media",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if strings.Contains(out, "hostNetwork: true") {
		t.Error("video chart must not enable hostNetwork")
	}
	if strings.Contains(out, "kind: Namespace") {
		t.Error("default render must not own Namespace; zonctl EnsureNamespace creates it")
	}
	if strings.Contains(out, "volumes:\n        - name: library\n          hostPath:") {
		t.Error("library volume must not use pod-level hostPath under Restricted PSA")
	}
	if !strings.Contains(out, "volumes:\n        - name: library\n          persistentVolumeClaim:\n            claimName: library") {
		t.Error("library volume must mount the library PVC")
	}
}

func TestLibraryStorageClassPVCWhenHostPathDisabled(t *testing.T) {
	out := render(t, "--set", "persistence.hostPath.enabled=false")
	if strings.Contains(out, "kind: PersistentVolume\n") {
		t.Error("hostPath.enabled=false must not render a static PersistentVolume")
	}
	if !strings.Contains(out, "kind: PersistentVolumeClaim") {
		t.Error("hostPath.enabled=false must still render a PVC")
	}
	if strings.Contains(out, "path: \"/data/zon/video/library\"") {
		t.Error("hostPath.enabled=false must not pin the host library path on a PV")
	}
}
