package mdns

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	paths   map[string]bool
	outputs map[string]string
	calls   []string
	fail    map[string]error
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if f.paths[file] {
		return "/usr/bin/" + file, nil
	}
	return "", errors.New("not found")
}

func (f *fakeRunner) CombinedOutput(_ context.Context, name string, args ...string) (string, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.calls = append(f.calls, key)
	if f.fail != nil {
		if err := f.fail[key]; err != nil {
			return f.outputs[key], err
		}
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return "", nil
}

type memFiles struct {
	data map[string][]byte
}

func (m *memFiles) ensure() {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
}

func (m *memFiles) MkdirAll(string, os.FileMode) error { m.ensure(); return nil }

func (m *memFiles) WriteFile(path string, data []byte, _ os.FileMode) error {
	m.ensure()
	m.data[path] = append([]byte(nil), data...)
	return nil
}

func (m *memFiles) ReadFile(path string) ([]byte, error) {
	m.ensure()
	d, ok := m.data[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return d, nil
}

func (m *memFiles) Remove(path string) error {
	m.ensure()
	delete(m.data, path)
	return nil
}

func TestApplyMissingPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteHostFile(t, root, "proc/sys/kernel/hostname", "appliance-01\n")
	m := &Manager{
		Root:     root,
		StateDir: "/state",
		Runner:   &fakeRunner{paths: map[string]bool{}},
		Files:    &memFiles{},
	}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonPackagesMissing {
		t.Fatalf("reason=%q", status.Reason)
	}
	if !status.Desired {
		t.Fatal("desired")
	}
	if status.AdvertisedName != "appliance-01.local" {
		t.Fatalf("advertisedName=%q", status.AdvertisedName)
	}
}

func TestApplyEnableStartsService(t *testing.T) {
	root := t.TempDir()
	mustWriteHostFile(t, root, "proc/sys/kernel/hostname", "Kitchen-Box.example.internal\n")
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "systemctl": true},
		outputs: map[string]string{
			"systemctl is-active avahi-daemon.service": "active",
			"systemctl unmask avahi-daemon.service":    "",
			"systemctl enable avahi-daemon.service":    "",
			"systemctl restart avahi-daemon.service":   "",
		},
	}
	m := &Manager{
		Root:     root,
		StateDir: "/state",
		Runner:   runner,
		Files:    &memFiles{},
	}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Actual != ActualActive {
		t.Fatalf("actual=%q status=%+v", status.Actual, status)
	}
	if status.AdvertisedName != "Kitchen-Box.local" {
		t.Fatalf("advertisedName=%q", status.AdvertisedName)
	}
}

func TestApplyDisableStopsService(t *testing.T) {
	root := t.TempDir()
	mustWriteHostFile(t, root, "proc/sys/kernel/hostname", "appliance-01.local\n")
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "systemctl": true},
		outputs: map[string]string{
			"systemctl is-active avahi-daemon.service": "inactive",
			"systemctl stop avahi-daemon.service":      "",
			"systemctl disable avahi-daemon.service":   "",
		},
	}
	m := &Manager{
		Root:     root,
		StateDir: "/state",
		Runner:   runner,
		Files:    &memFiles{},
	}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: false})
	if err != nil {
		t.Fatal(err)
	}
	if status.Desired {
		t.Fatal("desired should be false")
	}
	if status.Actual != ActualInactive {
		t.Fatalf("actual=%q", status.Actual)
	}
	if status.AdvertisedName != "appliance-01.local" {
		t.Fatalf("advertisedName=%q", status.AdvertisedName)
	}
}

func TestApplicationAliasesPreserveOperatorMappingsAndUseLANInterface(t *testing.T) {
	root := t.TempDir()
	avahiConfig := filepath.Join(root, "etc", "avahi", "avahi-daemon.conf")
	files := &memFiles{data: map[string][]byte{
		filepath.Join(root, "etc", "avahi", "hosts"): []byte("192.168.1.10 printer.local\n"),
		avahiConfig: []byte("[server]\nuse-ipv4=yes\n\n[publish]\n"),
	}}
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "avahi-publish-address": true, "avahi-resolve-host-name": true, "systemctl": true},
		outputs: map[string]string{
			"ip -4 route show default":                 "default via 192.168.1.1 dev enp1s0 proto dhcp\n",
			"hostname -I":                              "10.42.0.1 192.168.1.151\n",
			"systemctl is-active avahi-daemon.service": "active",
			"systemctl unmask avahi-daemon.service":    "",
			"systemctl enable avahi-daemon.service":    "",
			"systemctl restart avahi-daemon.service":   "",
		},
	}
	m := &Manager{Root: root, StateDir: "/state", Runner: runner, Files: files}
	request := ApplicationRequest{
		Application: "jellyfin",
		Services:    []ApplicationService{{Name: "jellyfin", ServiceType: "_jellyfin._tcp", Port: 8096}},
		Aliases:     []string{"jellyfin.local"},
	}
	if err := m.ApplyApplicationServices(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	hosts := string(files.data[filepath.Join(root, "etc", "avahi", "hosts")])
	if want := "192.168.1.10 printer.local"; !strings.Contains(hosts, want) {
		t.Fatalf("operator mapping missing from %q", hosts)
	}
	if strings.Contains(hosts, "jellyfin.local") {
		t.Fatalf("legacy static application alias remained in %q", hosts)
	}
	publisher := string(files.data[m.applicationAliasPublisherFile("jellyfin", "jellyfin.local")])
	if want := "ExecStart=/usr/bin/avahi-publish-address jellyfin.local 192.168.1.151"; !strings.Contains(publisher, want) {
		t.Fatalf("publisher unit missing %q from %q", want, publisher)
	}
	if config := string(files.data[avahiConfig]); !strings.Contains(config, "allow-interfaces=enp1s0\n") {
		t.Fatalf("Avahi configuration does not limit mDNS to the LAN interface: %q", config)
	}
	if err := m.ApplyApplicationServices(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	restarts := 0
	for _, call := range runner.calls {
		if call == "systemctl restart avahi-daemon.service" {
			restarts++
		}
	}
	if restarts != 1 {
		t.Fatalf("identical application reconcile restarted Avahi %d times, want 1", restarts)
	}
	if err := m.ApplyApplicationServices(context.Background(), ApplicationRequest{Application: "jellyfin"}); err != nil {
		t.Fatal(err)
	}
	hosts = string(files.data[filepath.Join(root, "etc", "avahi", "hosts")])
	if strings.Contains(hosts, "jellyfin.local") || !strings.Contains(hosts, "printer.local") {
		t.Fatalf("aliases were not safely withdrawn: %q", hosts)
	}
	if _, exists := files.data[m.applicationAliasPublisherFile("jellyfin", "jellyfin.local")]; exists {
		t.Fatal("alias publisher was not withdrawn")
	}
}

func TestApplicationAliasesFailWhenAvahiDoesNotPublishAlias(t *testing.T) {
	root := t.TempDir()
	files := &memFiles{data: map[string][]byte{
		filepath.Join(root, "etc", "avahi", "hosts"):             nil,
		filepath.Join(root, "etc", "avahi", "avahi-daemon.conf"): []byte("[server]\n\n[publish]\n"),
	}}
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "avahi-publish-address": true, "avahi-resolve-host-name": true, "systemctl": true},
		outputs: map[string]string{
			"ip -4 route show default":                 "default via 192.168.1.1 dev enp1s0 proto dhcp\n",
			"hostname -I":                              "192.168.1.151\n",
			"systemctl is-active avahi-daemon.service": "active",
			"systemctl unmask avahi-daemon.service":    "",
			"systemctl enable avahi-daemon.service":    "",
			"systemctl restart avahi-daemon.service":   "",
		},
		fail: map[string]error{"avahi-resolve-host-name -4 jellyfin.local": errors.New("timeout reached")},
	}
	m := &Manager{Root: root, StateDir: "/state", Runner: runner, Files: files}
	err := m.ApplyApplicationServices(context.Background(), ApplicationRequest{
		Application: "jellyfin",
		Aliases:     []string{"jellyfin.local"},
	})
	if err == nil || !strings.Contains(err.Error(), `application alias "jellyfin.local" was not published`) {
		t.Fatalf("ApplyApplicationServices error = %v", err)
	}
}

func mustWriteHostFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
