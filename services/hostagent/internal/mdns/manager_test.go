package mdns

import (
	"context"
	"errors"
	"os"
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
	m := &Manager{
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
}

func TestApplyEnableStartsService(t *testing.T) {
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "systemctl": true},
		outputs: map[string]string{
			"systemctl is-active avahi-daemon.service": "active",
			"systemctl unmask avahi-daemon.service":    "",
			"systemctl enable avahi-daemon.service":    "",
			"systemctl restart avahi-daemon.service":   "",
		},
	}
	m := &Manager{StateDir: "/state", Runner: runner, Files: &memFiles{}}
	status, err := m.Apply(context.Background(), ApplyRequest{Desired: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Actual != ActualActive {
		t.Fatalf("actual=%q status=%+v", status.Actual, status)
	}
}

func TestApplyDisableStopsService(t *testing.T) {
	runner := &fakeRunner{
		paths: map[string]bool{"avahi-daemon": true, "systemctl": true},
		outputs: map[string]string{
			"systemctl is-active avahi-daemon.service": "inactive",
			"systemctl stop avahi-daemon.service":      "",
			"systemctl disable avahi-daemon.service":   "",
		},
	}
	m := &Manager{StateDir: "/state", Runner: runner, Files: &memFiles{}}
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
}
