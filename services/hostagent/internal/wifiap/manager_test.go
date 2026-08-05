package wifiap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if f.outputs != nil {
		if out, ok := f.outputs[key]; ok {
			return out, nil
		}
		// Prefix match for pkill/pgrep patterns.
		for k, out := range f.outputs {
			if strings.HasPrefix(key, k) || strings.Contains(key, k) {
				return out, f.errors[k]
			}
		}
	}
	return "", nil
}

func TestApplyInactiveWhenNoHardware(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"hostapd": true, "dnsmasq": true, "iw": true},
		outputs: map[string]string{
			"iw dev": "",
		},
	}
	m := &Manager{
		ConfigDir:  "/cfg",
		StateDir:   "/state",
		RuntimeDir: "/run",
		Runner:     runner,
		Files:      files,
	}
	status, err := m.Apply(context.Background(), ApplyRequest{
		Desired:  true,
		PSK:      "long-enough-secret",
		SSIDBase: "kitchen",
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !status.Desired {
		t.Fatal("expected desired true")
	}
	if status.Actual != ActualInactive {
		t.Fatalf("actual = %q, want inactive", status.Actual)
	}
	if status.Reason != ReasonNoHardware {
		t.Fatalf("reason = %q, want %q", status.Reason, ReasonNoHardware)
	}
	if status.SSID != "kitchen-AP" {
		t.Fatalf("ssid = %q", status.SSID)
	}
}

func TestApplyActivatesFreeRadio(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"hostapd": true, "dnsmasq": true, "iw": true, "ip": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		ifindex 3
		wdev 0x1
		addr 00:11:22:33:44:55
		type managed
		wiphy 0
`,
			"iw list": `Wiphy phy0
	Band 1:
	Supported interface modes:
		 * managed
		 * AP
`,
			"iw dev wlan0 link": "Not connected.",
			"pgrep -f hostapd":  "123",
			"pgrep -f dnsmasq":  "124",
		},
	}
	// Fix pgrep keys to match patterns used.
	m := &Manager{
		ConfigDir:  "/cfg",
		StateDir:   "/state",
		RuntimeDir: "/run",
		Runner:     runner,
		Files:      files,
		PortBinder: FixedPortBinder{Allow: true},
	}
	// Override CombinedOutput behavior for pgrep after conf path is known.
	status, err := m.Apply(context.Background(), ApplyRequest{
		Desired:  true,
		PSK:      "long-enough-secret",
		SSIDBase: "kitchen",
	})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if status.SSID != "kitchen-AP" {
		t.Fatalf("ssid = %q", status.SSID)
	}
	if status.Iface != "wlan0" {
		t.Fatalf("iface = %q, want wlan0", status.Iface)
	}
	// Status re-runs pgrep; force active by stubbing all pgrep calls to succeed via prefix table.
	// Re-check with runner that always returns pids for pgrep.
	runner.outputs["pgrep"] = "1"
	// Call status with a runner that treats any pgrep as success
	runner2 := &matchingRunner{inner: runner}
	m.Runner = runner2
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Desired != true {
		t.Fatal("desired")
	}
	// Conf files should exist with mode secrets on hostapd
	if _, ok := files.data["/cfg/hostapd.conf"]; !ok {
		t.Fatal("expected hostapd.conf written")
	}
	if files.mode["/cfg/hostapd.conf"] != 0o600 {
		t.Fatalf("hostapd conf mode = %o", files.mode["/cfg/hostapd.conf"])
	}
	if !strings.Contains(string(files.data["/cfg/hostapd.conf"]), "ssid=kitchen-AP") {
		t.Fatalf("hostapd conf missing ssid: %s", files.data["/cfg/hostapd.conf"])
	}
	if conf, ok := files.data["/cfg/dnsmasq.conf"]; !ok {
		t.Fatal("expected dnsmasq.conf written")
	} else if strings.Contains(string(conf), "port=0") {
		t.Fatalf("when management :53 is free, dnsmasq must serve DNS:\n%s", conf)
	} else if !strings.Contains(string(conf), "address=/"+ManagementHostname+"/"+ManagementAddress) {
		t.Fatalf("dnsmasq missing manage.ap mapping:\n%s", conf)
	}
	if strings.Contains(string(files.data["/state/state.json"]), "long-enough") {
		t.Fatal("psk must not be stored in state.json")
	}
}

func TestApplyUsesDHCPOnlyWhenDNSPortBusy(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"hostapd": true, "dnsmasq": true, "iw": true, "ip": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		ifindex 3
		wdev 0x1
		addr 00:11:22:33:44:55
		type managed
		wiphy 0
`,
			"iw list": `Wiphy phy0
	Band 1:
	Supported interface modes:
		 * managed
		 * AP
`,
			"iw dev wlan0 link": "Not connected.",
		},
	}
	m := &Manager{
		ConfigDir:  "/cfg",
		StateDir:   "/state",
		RuntimeDir: "/run",
		Runner:     runner,
		Files:      files,
		PortBinder: FixedPortBinder{Allow: false},
	}
	if _, err := m.Apply(context.Background(), ApplyRequest{
		Desired:  true,
		PSK:      "long-enough-secret",
		SSIDBase: "kitchen",
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	conf := string(files.data["/cfg/dnsmasq.conf"])
	if !strings.Contains(conf, "port=0") {
		t.Fatalf("when :53 is busy, dnsmasq must use port=0:\n%s", conf)
	}
}

func TestRenderDnsmasqConf(t *testing.T) {
	withDNS := renderDnsmasqConf("wlan0", true)
	if !strings.Contains(withDNS, "listen-address="+ManagementAddress) {
		t.Fatalf("expected listen-address:\n%s", withDNS)
	}
	if strings.Contains(withDNS, "port=0") {
		t.Fatalf("unexpected port=0:\n%s", withDNS)
	}
	dhcpOnly := renderDnsmasqConf("wlan0", false)
	if !strings.Contains(dhcpOnly, "port=0") {
		t.Fatalf("expected port=0:\n%s", dhcpOnly)
	}
}

// matchingRunner matches command name prefixes for fakes.
type matchingRunner struct {
	inner *fakeRunner
}

func (m *matchingRunner) LookPath(file string) (string, error) { return m.inner.LookPath(file) }

func (m *matchingRunner) CombinedOutput(ctx context.Context, name string, args ...string) (string, error) {
	if name == "pgrep" {
		return "99", nil
	}
	if name == "pkill" {
		return "", nil
	}
	return m.inner.CombinedOutput(ctx, name, args...)
}

func TestApplyDisableCleansGeneratedConfigs(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"hostapd": true, "dnsmasq": true, "iw": true, "ip": true, "pkill": true, "pgrep": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		type managed
		wiphy 0
`,
			"iw list": `Wiphy phy0
	Supported interface modes:
		 * managed
		 * AP
`,
			"iw dev wlan0 link": "Not connected.",
		},
	}
	m := &Manager{
		ConfigDir:  "/cfg",
		StateDir:   "/state",
		RuntimeDir: "/run",
		Runner:     runner,
		Files:      files,
		PortBinder: FixedPortBinder{Allow: true},
	}
	if _, err := m.Apply(context.Background(), ApplyRequest{
		Desired:  true,
		PSK:      "long-enough-secret",
		SSIDBase: "kitchen",
	}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, ok := files.data["/cfg/hostapd.conf"]; !ok {
		t.Fatal("expected hostapd conf after enable")
	}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: false})
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if status.Desired {
		t.Fatal("expected desired false after disable")
	}
	if _, ok := files.data["/cfg/hostapd.conf"]; ok {
		t.Fatal("disable must remove hostapd.conf")
	}
	if _, ok := files.data["/cfg/dnsmasq.conf"]; ok {
		t.Fatal("disable must remove dnsmasq.conf")
	}
	if _, ok := files.data["/state/psk"]; ok {
		t.Fatal("disable must remove psk")
	}
}

func TestApplyRejectsSoftWhenRadioInUse(t *testing.T) {
	files := newMemFile()
	runner := &fakeRunner{
		paths: map[string]bool{"hostapd": true, "dnsmasq": true, "iw": true},
		outputs: map[string]string{
			"iw dev": `phy#0
	Interface wlan0
		type managed
		wiphy 0
`,
			"iw list": `Wiphy phy0
	Supported interface modes:
		 * managed
		 * AP
`,
			"iw dev wlan0 link": "Connected to aa:bb:cc:dd:ee:ff (on wlan0)\n\tSSID: home-lan\n",
		},
	}
	m := &Manager{ConfigDir: "/cfg", StateDir: "/state", RuntimeDir: "/run", Runner: runner, Files: files}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true, PSK: "long-enough-secret", SSIDBase: "box"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonRadioInUse {
		t.Fatalf("reason = %q, want radio_in_use", status.Reason)
	}
	if status.Actual != ActualInactive {
		t.Fatalf("actual = %q", status.Actual)
	}
}
