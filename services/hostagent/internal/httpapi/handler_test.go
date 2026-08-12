package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/wifiap"
	"appliance-code/services/hostagent/internal/wificlient"
)

type wifiClientStub struct {
	status wificlient.Status
	scan   wificlient.ScanResult
	apply  wificlient.ApplyRequest
}

func (s *wifiClientStub) Status(context.Context) (wificlient.Status, error) { return s.status, nil }

func (s *wifiClientStub) Enable(context.Context) (wificlient.Status, error) {
	s.status.RadioEnabled = true
	return s.status, nil
}

func (s *wifiClientStub) Apply(_ context.Context, req wificlient.ApplyRequest) (wificlient.Status, error) {
	s.apply = req
	return s.status, nil
}

func (s *wifiClientStub) Scan(context.Context) (wificlient.ScanResult, error) { return s.scan, nil }

type wifiAPStub struct {
	status wifiap.Status
	apply  wifiap.ApplyRequest
}

func (s *wifiAPStub) Status(context.Context) (wifiap.Status, error) { return s.status, nil }

func (s *wifiAPStub) Apply(_ context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	s.apply = req
	return s.status, nil
}

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

func TestHandlerRejectsConflictingClientWifiAndAPEnablement(t *testing.T) {
	wifi := &wifiClientStub{status: wificlient.Status{Actual: wificlient.ActualInactive, Reason: wificlient.ReasonDesiredOff}}
	ap := &wifiAPStub{status: wifiap.Status{Desired: true, Actual: wifiap.ActualActive}}
	handler := NewHandlerWithControllers(bridge.Local{}, wifi, ap, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/internal/v1/host/wifi/enable", nil))
	if rec.Code != http.StatusOK || wifi.status.RadioEnabled {
		t.Fatalf("client enable conflict status=%d, radio=%t", rec.Code, wifi.status.RadioEnabled)
	}
	var clientStatus wificlient.Status
	if err := json.NewDecoder(rec.Body).Decode(&clientStatus); err != nil {
		t.Fatal(err)
	}
	if clientStatus.Reason != wificlient.ReasonRadioInUseByAP || !strings.Contains(clientStatus.Message, "Disable Wi-Fi AP first") {
		t.Fatalf("client conflict=%+v", clientStatus)
	}

	wifi.status = wificlient.Status{Desired: true, Actual: wificlient.ActualActive}
	ap.status = wifiap.Status{Desired: false, Actual: wifiap.ActualInactive}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/internal/v1/host/wifi-ap", strings.NewReader(`{"desired":true,"psk":"long-enough-secret"}`)))
	if rec.Code != http.StatusOK || ap.apply.Desired {
		t.Fatalf("AP enable conflict status=%d, apply=%+v", rec.Code, ap.apply)
	}
	var apStatus wifiap.Status
	if err := json.NewDecoder(rec.Body).Decode(&apStatus); err != nil {
		t.Fatal(err)
	}
	if apStatus.Reason != wifiap.ReasonRadioInUse || !strings.Contains(apStatus.Message, "Disable client Wi-Fi first") {
		t.Fatalf("AP conflict=%+v", apStatus)
	}
}

func TestHandlerServesClientWifiWorkflowEndpoints(t *testing.T) {
	wifi := &wifiClientStub{
		status: wificlient.Status{
			Desired:          false,
			Actual:           wificlient.ActualInactive,
			Reason:           wificlient.ReasonDesiredOff,
			Security:         wificlient.SecurityUnknown,
			SupportedCapable: true,
		},
		scan: wificlient.ScanResult{
			Iface:            "wlan0",
			SupportedCapable: true,
			Networks: []wificlient.ScanNetwork{{
				SSID:             "office-lan",
				Security:         wificlient.SecurityWPA2PSK,
				RequiresPassword: true,
				Connectable:      true,
				SignalDBM:        -42,
			}},
		},
	}
	handler := NewHandlerWithControllers(bridge.Local{}, wifi, nil, nil)

	for _, path := range []string{"/internal/v1/host/wifi", "/internal/v1/host/wifi/scan"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/internal/v1/host/wifi/enable", nil))
	if rec.Code != http.StatusOK || !wifi.status.RadioEnabled {
		t.Fatalf("enable status = %d, radio enabled = %t", rec.Code, wifi.status.RadioEnabled)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/internal/v1/host/wifi", strings.NewReader(`{"desired":true,"ssid":"office-lan","psk":"long-enough-secret","security":"wpa2-psk"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rec.Code)
	}
	if !wifi.apply.Desired || wifi.apply.SSID != "office-lan" || wifi.apply.Security != wificlient.SecurityWPA2PSK {
		t.Fatalf("apply request = %+v", wifi.apply)
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
