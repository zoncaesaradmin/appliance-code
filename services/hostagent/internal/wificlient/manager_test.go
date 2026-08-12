package wificlient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/hostagent/internal/wifiap"
)

type memFile struct {
	data map[string][]byte
	mode map[string]os.FileMode
}

func newMemFile() *memFile {
	return &memFile{data: map[string][]byte{}, mode: map[string]os.FileMode{}}
}

func (m *memFile) MkdirAll(string, os.FileMode) error { return nil }
func (m *memFile) WriteFile(path string, data []byte, perm os.FileMode) error {
	cp := append([]byte(nil), data...)
	m.data[path] = cp
	m.mode[path] = perm
	return nil
}
func (m *memFile) ReadFile(path string) ([]byte, error) {
	data, ok := m.data[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (m *memFile) Remove(path string) error {
	delete(m.data, path)
	delete(m.mode, path)
	return nil
}
func (m *memFile) Stat(path string) (os.FileInfo, error) {
	data, ok := m.data[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return fakeInfo{name: filepath.Base(path), size: int64(len(data)), mode: m.mode[path]}, nil
}

type fakeInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

type fakeRunner struct {
	paths   map[string]bool
	outputs map[string]string
	errors  map[string]error
	calls   []string
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if f.paths != nil && f.paths[file] {
		return "/usr/sbin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f *fakeRunner) CombinedOutput(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if f.errors != nil {
		if err, ok := f.errors[key]; ok {
			return f.outputs[key], err
		}
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	for k, out := range f.outputs {
		if strings.HasPrefix(key, k) || strings.Contains(key, k) {
			return out, f.errors[k]
		}
	}
	return "", nil
}

var _ wifiap.FileIO = (*memFile)(nil)
var _ wifiap.Runner = (*fakeRunner)(nil)

func TestApplyNeedsSSID(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"iw": true, "ip": true, "wpa_supplicant": true, "dhclient": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		ifindex 3
		type managed
		wiphy 0
`,
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonSSIDMissing {
		t.Fatalf("reason=%q", status.Reason)
	}
	if status.Desired {
		t.Fatalf("invalid request must not enable Wi-Fi: %+v", status)
	}
}

func TestApplyRejectsInvalidPasswordWithoutChangingDesiredState(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"iw": true, "ip": true, "wpa_supplicant": true, "dhclient": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		type managed
		wiphy 0
`,
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true, SSID: "office", PSK: "short", Security: SecurityWPA2PSK})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonInvalidPSK || status.Desired {
		t.Fatalf("status=%+v, want invalid password with desired off", status)
	}
	if _, ok := files.data["/cfg/wpa_supplicant.conf"]; ok {
		t.Fatal("invalid request must not write Wi-Fi configuration")
	}
}

func TestApplyReportsConnectingUntilAssociationAndDHCPComplete(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"iw": true, "ip": true, "wpa_supplicant": true, "dhclient": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		type managed
		wiphy 0
`,
			"pgrep -f wpa_supplicant": "101",
			"pgrep -f dhclient":       "102",
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true, SSID: "office", PSK: "long-enough-secret", Security: SecurityWPA2PSK})
	if err != nil {
		t.Fatal(err)
	}
	if status.Actual != ActualConnecting || status.Reason != ReasonConnectionPending {
		t.Fatalf("status=%+v, want connecting", status)
	}
	if !strings.Contains(strings.Join(runner.calls, "\n"), "dhclient -nw") {
		t.Fatalf("dhclient must start in the background: %v", runner.calls)
	}
}

func TestApplyActivatesClientWifi(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"iw": true, "iwlist": true, "ip": true, "wpa_supplicant": true, "dhclient": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlp2s0
		ifindex 3
		type managed
		wiphy 0
	Interface wlan0
		ifindex 4
		type AP
		wiphy 1
`,
			"iw list": `Wiphy phy0
	Supported interface modes:
		 * managed
Wiphy phy1
	Supported interface modes:
		 * managed
		 * AP
`,
			"iw dev wlp2s0 link":            "Connected to aa:bb:cc:dd:ee:ff (on wlp2s0)\n\tSSID: home-lan\n",
			"ip -4 -o addr show dev wlp2s0": "3: wlp2s0    inet 192.168.1.155/24 brd 192.168.1.255 scope global dynamic wlp2s0\n",
			"pgrep -f wpa_supplicant":       "101",
			"pgrep -f dhclient":             "102",
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	status, err := m.Apply(context.Background(), ApplyRequest{
		Desired:  true,
		SSID:     "home-lan",
		PSK:      "long-enough-secret",
		Security: SecurityWPA2PSK,
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if status.Actual != ActualActive {
		t.Fatalf("actual=%q status=%+v", status.Actual, status)
	}
	if status.Iface != "wlp2s0" {
		t.Fatalf("iface=%q", status.Iface)
	}
	if !status.SupportsConcurrentAP {
		t.Fatalf("expected concurrent AP support: %+v", status)
	}
	conf := string(files.data["/cfg/wpa_supplicant.conf"])
	if !strings.Contains(conf, `ssid="home-lan"`) {
		t.Fatalf("config missing ssid: %s", conf)
	}
	if !strings.Contains(conf, `psk="long-enough-secret"`) {
		t.Fatalf("config missing psk: %s", conf)
	}
}

func TestScanNetworks(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"iw": true, "ip": true, "wpa_supplicant": true, "dhclient": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlp2s0
		ifindex 3
		type managed
		wiphy 0
`,
			"iw list": `Wiphy phy0
	Supported interface modes:
		 * managed
`,
			"iw dev wlp2s0 scan": `BSS aa:bb:cc:dd:ee:ff(on wlp2s0)
	signal: -42.00 dBm
	SSID: home-lan
	RSN:
BSS ff:ee:dd:cc:bb:aa(on wlp2s0)
	signal: -60.00 dBm
	SSID: guest
`,
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	result, err := m.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Networks) != 2 {
		t.Fatalf("networks=%d", len(result.Networks))
	}
	if result.Networks[0].SSID != "home-lan" || !result.Networks[0].RequiresPassword {
		t.Fatalf("unexpected first network: %+v", result.Networks[0])
	}
	if result.Networks[1].Security != SecurityOpen {
		t.Fatalf("unexpected guest security: %+v", result.Networks[1])
	}
	if !result.Networks[0].Connectable || !result.Networks[1].Connectable {
		t.Fatalf("expected scanned personal/open networks to be connectable: %+v", result.Networks)
	}
	if !strings.Contains(strings.Join(runner.calls, "\n"), "ip link set wlp2s0 up") {
		t.Fatalf("scan must bring the Wi-Fi interface up: %v", runner.calls)
	}
}

func TestScanMarksEnterpriseNetworksUnsupported(t *testing.T) {
	result, err := scanNetworks(context.Background(), &fakeRunner{outputs: map[string]string{
		"iw dev wlan0 scan": `BSS aa:bb:cc:dd:ee:ff(on wlan0)
	signal: -42.00 dBm
	SSID: office-enterprise
	RSN:
		Authentication suites:
			* 00-0f-ac:1
`,
	}}, "wlan0")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Connectable || result[0].Security != SecurityEnterprise {
		t.Fatalf("enterprise scan result=%+v", result)
	}
}
