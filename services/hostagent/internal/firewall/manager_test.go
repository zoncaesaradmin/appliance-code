package firewall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testRunner struct {
	program string
}

func (r *testRunner) LookPath(string) (string, error) { return "nft", nil }
func (r *testRunner) CombinedOutput(_ context.Context, _ string, args ...string) (string, error) {
	data, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		return "", err
	}
	r.program = string(data)
	return "", nil
}

func TestApplyOwnsOnlyApplicationTableAndBlocksManagementNetwork(t *testing.T) {
	runner := &testRunner{}
	m := &Manager{StateDir: t.TempDir(), ManagementCIDR: "10.42.0.0/24", Runner: runner, Files: OSFileIO{}}
	status, err := m.Apply(context.Background(), ApplicationPolicy{
		Application: "camera",
		Endpoints:   []Endpoint{{Name: "stream", Protocol: "TCP", Port: 8096}, {Name: "discovery", Protocol: "UDP", Port: 1900}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Actual != ActualActive {
		t.Fatalf("status = %+v", status)
	}
	for _, want := range []string{
		"destroy table inet zon_applications",
		"ip saddr 10.42.0.0/24 tcp dport { 8096 } drop",
		"ip saddr 10.42.0.0/24 udp dport { 1900 } drop",
	} {
		if !strings.Contains(runner.program, want) {
			t.Fatalf("nft program missing %q:\n%s", want, runner.program)
		}
	}
	if strings.Contains(runner.program, "dport 22") {
		t.Fatalf("application table must not change SSH:\n%s", runner.program)
	}
	if _, err := os.Stat(filepath.Join(m.StateDir, "policies.json")); err != nil {
		t.Fatalf("expected durable policy state: %v", err)
	}
}

func TestApplyRejectsInvalidPolicyBeforeNft(t *testing.T) {
	runner := &testRunner{}
	m := &Manager{StateDir: t.TempDir(), Runner: runner, Files: OSFileIO{}}
	status, err := m.Apply(context.Background(), ApplicationPolicy{Application: "camera", Endpoints: []Endpoint{{Name: "bad", Protocol: "SCTP", Port: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonInvalidPolicy {
		t.Fatalf("status = %+v", status)
	}
	if runner.program != "" {
		t.Fatal("invalid policy must not invoke nft")
	}
}

func TestApplyReportsMissingNftWithoutPersistingPolicy(t *testing.T) {
	m := &Manager{StateDir: t.TempDir(), Runner: missingNftRunner{}, Files: OSFileIO{}}
	status, err := m.Apply(context.Background(), ApplicationPolicy{Application: "camera", Endpoints: []Endpoint{{Name: "stream", Protocol: "TCP", Port: 8096}}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ReasonNftUnavailable {
		t.Fatalf("status = %+v", status)
	}
}

type missingNftRunner struct{}

func (missingNftRunner) LookPath(string) (string, error) { return "", errors.New("not found") }
func (missingNftRunner) CombinedOutput(context.Context, string, ...string) (string, error) {
	return "", errors.New("should not execute")
}
