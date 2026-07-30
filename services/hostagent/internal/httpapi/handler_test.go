package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"appliance-code/services/hostagent/internal/bridge"
)

func TestHandlerServesHostEndpoints(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "etc/hostname"), "stale-static-name\n")
	mustWriteFile(t, filepath.Join(root, "etc/os-release"), "PRETTY_NAME=\"Ubuntu 24.04.2 LTS\"\n")
	mustWriteFile(t, filepath.Join(root, "proc/uptime"), "123.45 999.00\n")
	mustWriteFile(t, filepath.Join(root, "proc/loadavg"), "0.10 0.20 0.30 1/100 42\n")
	mustWriteFile(t, filepath.Join(root, "proc/meminfo"), "MemTotal:       1024 kB\nMemAvailable:    512 kB\n")
	mustWriteFile(t, filepath.Join(root, "proc/version"), "Linux version 6.8.0-test\n")
	mustWriteFile(t, filepath.Join(root, "proc/sys/kernel/hostname"), "live-hostname\n")
	mustWriteFile(t, filepath.Join(root, "proc/sys/kernel/osrelease"), "6.8.0-live\n")

	handler := NewHandler(bridge.Local{Root: root})
	for _, path := range []string{"/healthz", "/internal/v1/host/stats"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}

	infoReq := httptest.NewRequest(http.MethodGet, "/internal/v1/host/info", nil)
	infoRec := httptest.NewRecorder()
	handler.ServeHTTP(infoRec, infoReq)
	if infoRec.Code != http.StatusOK {
		t.Fatalf("info status = %d, want 200", infoRec.Code)
	}
	var info struct {
		Hostname      string `json:"hostname"`
		KernelVersion string `json:"kernelVersion"`
		Architecture  string `json:"architecture"`
	}
	if err := json.NewDecoder(infoRec.Body).Decode(&info); err != nil {
		t.Fatalf("decode info response: %v", err)
	}
	if info.Hostname != "live-hostname" {
		t.Fatalf("info hostname = %q, want live hostname", info.Hostname)
	}
	if info.KernelVersion != "6.8.0-live" {
		t.Fatalf("info kernelVersion = %q, want live kernel version", info.KernelVersion)
	}
	if info.Architecture != runtime.GOARCH {
		t.Fatalf("info architecture = %q, want runtime architecture %q", info.Architecture, runtime.GOARCH)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/internal/v1/host/health", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", healthRec.Code)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
