package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Runner is intentionally command-shaped so tests can verify the generated
// nftables program without changing the machine firewall.
type Runner interface {
	LookPath(file string) (string, error)
	CombinedOutput(ctx context.Context, name string, args ...string) (string, error)
}

type FileIO interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
}

type OSFileIO struct{}

func (OSFileIO) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileIO) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (OSFileIO) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// Manager owns only the inet zon_applications table. It never flushes or
// modifies a distribution, operator, K3s, or application-owned table.
type Manager struct {
	StateDir       string
	ManagementCIDR string
	Runner         Runner
	Files          FileIO
}

func (m *Manager) stateDir() string {
	if strings.TrimSpace(m.StateDir) == "" {
		return DefaultStateDir
	}
	return m.StateDir
}

func (m *Manager) managementCIDR() string {
	if strings.TrimSpace(m.ManagementCIDR) == "" {
		return DefaultManagementCIDR
	}
	return m.ManagementCIDR
}

func (m *Manager) files() FileIO {
	if m.Files != nil {
		return m.Files
	}
	return OSFileIO{}
}

func (m *Manager) statePath() string { return filepath.Join(m.stateDir(), "policies.json") }

func (m *Manager) Apply(ctx context.Context, policy ApplicationPolicy) (Status, error) {
	if err := validatePolicy(policy); err != nil {
		return Status{Application: policy.Application, Actual: ActualFailed, Reason: ReasonInvalidPolicy, Message: err.Error()}, nil
	}
	policies, err := m.load()
	if err != nil {
		return Status{}, err
	}
	if len(policy.Endpoints) == 0 {
		delete(policies, policy.Application)
	} else {
		policies[policy.Application] = policy
	}
	if err := m.applyRules(ctx, policies); err != nil {
		reason := ReasonApplyFailed
		if strings.Contains(err.Error(), "nft is not installed") {
			reason = ReasonNftUnavailable
		}
		return Status{Application: policy.Application, Actual: ActualFailed, Reason: reason, Message: err.Error()}, nil
	}
	if err := m.save(policies); err != nil {
		return Status{}, err
	}
	actual := ActualActive
	message := "application endpoints are allowed on trusted networks and blocked on the management access point"
	if len(policy.Endpoints) == 0 {
		actual = ActualInactive
		message = "application endpoint exposure withdrawn"
	}
	return Status{Application: policy.Application, Actual: actual, Message: message}, nil
}

func (m *Manager) Status(_ context.Context, application string) (Status, error) {
	policies, err := m.load()
	if err != nil {
		return Status{}, err
	}
	if _, ok := policies[application]; !ok {
		return Status{Application: application, Actual: ActualInactive, Message: "no direct application endpoints are exposed"}, nil
	}
	return Status{Application: application, Actual: ActualActive, Message: "application endpoint policy is persisted"}, nil
}

// Reconcile restores only previously persisted application endpoint rules after
// a reboot. With no installed application endpoints it is a no-op, preserving
// the existing no-app appliance firewall behavior.
func (m *Manager) Reconcile(ctx context.Context) error {
	policies, err := m.load()
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	return m.applyRules(ctx, policies)
}

func (m *Manager) load() (map[string]ApplicationPolicy, error) {
	data, err := m.files().ReadFile(m.statePath())
	if os.IsNotExist(err) {
		return map[string]ApplicationPolicy{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("application firewall: read state: %w", err)
	}
	var policies map[string]ApplicationPolicy
	if err := json.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("application firewall: parse state: %w", err)
	}
	if policies == nil {
		policies = map[string]ApplicationPolicy{}
	}
	return policies, nil
}

func (m *Manager) save(policies map[string]ApplicationPolicy) error {
	if err := m.files().MkdirAll(m.stateDir(), 0o700); err != nil {
		return fmt.Errorf("application firewall: create state directory: %w", err)
	}
	data, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return fmt.Errorf("application firewall: encode state: %w", err)
	}
	if err := m.files().WriteFile(m.statePath(), data, 0o600); err != nil {
		return fmt.Errorf("application firewall: write state: %w", err)
	}
	return nil
}

func (m *Manager) applyRules(ctx context.Context, policies map[string]ApplicationPolicy) error {
	if m.Runner == nil {
		return fmt.Errorf("application firewall: runner is required")
	}
	if _, err := m.Runner.LookPath("nft"); err != nil {
		return fmt.Errorf("application firewall: nft is not installed")
	}
	program, err := renderProgram(m.managementCIDR(), policies)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "zon-application-firewall-*.nft")
	if err != nil {
		return fmt.Errorf("application firewall: create nft program: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(program); err != nil {
		tmp.Close()
		return fmt.Errorf("application firewall: write nft program: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("application firewall: close nft program: %w", err)
	}
	if _, err := m.Runner.CombinedOutput(ctx, "nft", "-f", tmpPath); err != nil {
		return fmt.Errorf("application firewall: apply nft rules: %w", err)
	}
	return nil
}

func renderProgram(managementCIDR string, policies map[string]ApplicationPolicy) (string, error) {
	prefix, err := netip.ParsePrefix(managementCIDR)
	if err != nil || !prefix.Addr().Is4() {
		return "", fmt.Errorf("application firewall: invalid management CIDR %q", managementCIDR)
	}
	var tcpPorts, udpPorts []int
	for _, policy := range policies {
		for _, endpoint := range policy.Endpoints {
			switch endpoint.Protocol {
			case "TCP":
				tcpPorts = append(tcpPorts, endpoint.Port)
			case "UDP":
				udpPorts = append(udpPorts, endpoint.Port)
			}
		}
	}
	sort.Ints(tcpPorts)
	sort.Ints(udpPorts)
	tcpPorts = dedupe(tcpPorts)
	udpPorts = dedupe(udpPorts)
	var lines []string
	// destroy is idempotent when the table is absent, and nft applies this whole
	// file as one transaction. The previous policy therefore remains in force
	// if the replacement program is rejected.
	lines = append(lines, "destroy table inet zon_applications", "table inet zon_applications {", "  chain input {", "    type filter hook input priority -10; policy accept;")
	if len(tcpPorts) > 0 {
		lines = append(lines, fmt.Sprintf("    ip saddr %s tcp dport { %s } drop", prefix, ports(tcpPorts)))
	}
	if len(udpPorts) > 0 {
		lines = append(lines, fmt.Sprintf("    ip saddr %s udp dport { %s } drop", prefix, ports(udpPorts)))
	}
	lines = append(lines, "  }", "}", "")
	return strings.Join(lines, "\n"), nil
}

func validatePolicy(policy ApplicationPolicy) error {
	if !validName(policy.Application) {
		return fmt.Errorf("application firewall: application must be a DNS label")
	}
	for _, endpoint := range policy.Endpoints {
		if !validName(endpoint.Name) || (endpoint.Protocol != "TCP" && endpoint.Protocol != "UDP") || endpoint.Port < 1 || endpoint.Port > 65535 {
			return fmt.Errorf("application firewall: endpoint must have a valid name, TCP/UDP protocol, and port")
		}
	}
	return nil
}

func validName(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r == '-' && i > 0 && i < len(value)-1) {
			continue
		}
		return false
	}
	return true
}

func ports(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, ", ")
}

func dedupe(values []int) []int {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
