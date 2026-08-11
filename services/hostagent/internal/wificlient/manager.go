package wificlient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"appliance-code/services/hostagent/internal/wifiap"
)

type Manager struct {
	ConfigDir  string
	StateDir   string
	RuntimeDir string
	Runner     wifiap.Runner
	Files      wifiap.FileIO
}

func NewManager() *Manager {
	return &Manager{
		ConfigDir:  DefaultConfigDir,
		StateDir:   DefaultStateDir,
		RuntimeDir: DefaultRuntimeDir,
		Runner:     wifiap.ExecRunner{},
		Files:      wifiap.OSFileIO{},
	}
}

func (m *Manager) paths() (configDir, stateDir, runtimeDir string) {
	configDir = strings.TrimSpace(m.ConfigDir)
	if configDir == "" {
		configDir = DefaultConfigDir
	}
	stateDir = strings.TrimSpace(m.StateDir)
	if stateDir == "" {
		stateDir = DefaultStateDir
	}
	runtimeDir = strings.TrimSpace(m.RuntimeDir)
	if runtimeDir == "" {
		runtimeDir = DefaultRuntimeDir
	}
	return configDir, stateDir, runtimeDir
}

func (m *Manager) runner() wifiap.Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return wifiap.ExecRunner{}
}

func (m *Manager) files() wifiap.FileIO {
	if m.Files != nil {
		return m.Files
	}
	return wifiap.OSFileIO{}
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	inv, err := inspectRadios(ctx, m.runner())
	if err != nil {
		return Status{}, err
	}
	supportsConcurrentAP, concurrentAPDetail := inv.concurrentAPSupport()
	status := Status{
		Desired:              st.Desired,
		Actual:               ActualInactive,
		SSID:                 st.SSID,
		Iface:                st.Iface,
		Security:             defaultSecurity(st.Security),
		SupportedCapable:     inv.supportedCapable(),
		SupportsConcurrentAP: supportsConcurrentAP,
		ConcurrentAPDetail:   concurrentAPDetail,
	}
	if status.Iface == "" {
		status.Iface = inv.defaultManagedIface()
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		if st.Desired {
			status.Reason = ReasonPackagesMissing
			status.Message = "required packages (wpa_supplicant/dhclient/iw) are not installed"
		} else {
			status.Reason = ReasonDesiredOff
			status.Message = "client Wi-Fi is not desired"
		}
		return status, nil
	}
	if !st.Desired {
		status.Reason = ReasonDesiredOff
		status.Message = "client Wi-Fi is not desired"
		return status, nil
	}
	if status.Iface == "" {
		status.Reason = inv.unavailableReason()
		status.Message = unavailableMessage(status.Reason)
		return status, nil
	}
	linkSSID, connected, _ := linkState(ctx, m.runner(), status.Iface)
	if status.SSID == "" {
		status.SSID = linkSSID
	}
	status.IPv4Addresses, _ = ipv4Addresses(ctx, m.runner(), status.Iface)
	if connected && len(status.IPv4Addresses) > 0 {
		status.Actual = ActualActive
		status.Reason = ReasonNone
		status.Message = fmt.Sprintf("client Wi-Fi is active on %s", status.Iface)
		return status, nil
	}
	servicesRunning := m.servicesActive(ctx, status.Iface)
	if connected && len(status.IPv4Addresses) == 0 {
		status.Actual = ActualFailed
		status.Reason = ReasonDHCPFailed
		status.Message = "client Wi-Fi is associated but has no IPv4 address"
		return status, nil
	}
	if servicesRunning {
		status.Reason = ReasonNotConfigured
		status.Message = "client Wi-Fi connection is in progress or waiting for association"
		return status, nil
	}
	status.Reason = ReasonConnectionFailed
	status.Message = "client Wi-Fi is desired but not connected"
	return status, nil
}

func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (Status, error) {
	configDir, stateDir, runtimeDir := m.paths()
	if err := m.files().MkdirAll(configDir, 0o755); err != nil {
		return Status{}, fmt.Errorf("wificlient: create config dir: %w", err)
	}
	if err := m.files().MkdirAll(stateDir, 0o700); err != nil {
		return Status{}, fmt.Errorf("wificlient: create state dir: %w", err)
	}
	if err := m.files().MkdirAll(runtimeDir, 0o755); err != nil {
		return Status{}, fmt.Errorf("wificlient: create runtime dir: %w", err)
	}
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	st.Desired = req.Desired
	if !req.Desired {
		if err := m.teardown(ctx); err != nil {
			_ = m.saveState(st)
			return Status{}, fmt.Errorf("wificlient: disable: %w", err)
		}
		st.SSID = ""
		st.Iface = ""
		st.Security = ""
		if err := m.saveState(st); err != nil {
			return Status{}, err
		}
		_ = m.files().Remove(m.confPath())
		_ = m.files().Remove(m.dhcpLeasePath())
		_ = m.files().Remove(m.wpaPidPath())
		_ = m.files().Remove(m.dhcpPidPath())
		return m.Status(ctx)
	}
	ssid := strings.TrimSpace(req.SSID)
	if ssid == "" {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonSSIDMissing
		status.Message = "an SSID is required to enable client Wi-Fi"
		return status, nil
	}
	if !packagesPresent(m.runner()) {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonPackagesMissing
		status.Message = "required packages (wpa_supplicant/dhclient/iw) are not installed"
		return status, nil
	}
	inv, err := inspectRadios(ctx, m.runner())
	if err != nil {
		return Status{}, err
	}
	iface := inv.defaultManagedIface()
	if iface == "" {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = inv.unavailableReason()
		status.Message = unavailableMessage(status.Reason)
		return status, nil
	}
	st.SSID = ssid
	st.Iface = iface
	st.Security = resolveSecurity(req.Security, req.PSK)
	if err := m.saveState(st); err != nil {
		return Status{}, err
	}
	if err := m.prepareInterface(ctx, iface); err != nil {
		return Status{}, err
	}
	if err := m.writeConfig(st, strings.TrimSpace(req.PSK)); err != nil {
		return Status{}, err
	}
	if err := m.startServices(ctx, iface); err != nil {
		return Status{}, err
	}
	return m.Status(ctx)
}

func (m *Manager) Scan(ctx context.Context) (ScanResult, error) {
	inv, err := inspectRadios(ctx, m.runner())
	if err != nil {
		return ScanResult{}, err
	}
	supportsConcurrentAP, concurrentAPDetail := inv.concurrentAPSupport()
	result := ScanResult{
		Iface:                inv.defaultManagedIface(),
		SupportedCapable:     inv.supportedCapable(),
		SupportsConcurrentAP: supportsConcurrentAP,
		ConcurrentAPDetail:   concurrentAPDetail,
	}
	if !packagesPresent(m.runner()) {
		result.SupportedCapable = false
		result.Reason = ReasonPackagesMissing
		result.Message = "required packages (wpa_supplicant/dhclient/iw) are not installed"
		return result, nil
	}
	if result.Iface == "" {
		result.Reason = inv.unavailableReason()
		result.Message = unavailableMessage(result.Reason)
		return result, nil
	}
	networks, err := scanNetworks(ctx, m.runner(), result.Iface)
	if err != nil {
		result.Reason = ReasonScanFailed
		result.Message = err.Error()
		return result, nil
	}
	result.Networks = networks
	if len(networks) == 0 {
		result.Message = "no Wi-Fi networks were discovered"
	}
	return result, nil
}

func (m *Manager) statePath() string {
	_, stateDir, _ := m.paths()
	return filepath.Join(stateDir, "state.json")
}

func (m *Manager) confPath() string {
	configDir, _, _ := m.paths()
	return filepath.Join(configDir, "wpa_supplicant.conf")
}

func (m *Manager) wpaPidPath() string {
	_, _, runtimeDir := m.paths()
	return filepath.Join(runtimeDir, "wpa_supplicant.pid")
}

func (m *Manager) dhcpPidPath() string {
	_, _, runtimeDir := m.paths()
	return filepath.Join(runtimeDir, "dhclient.pid")
}

func (m *Manager) dhcpLeasePath() string {
	_, stateDir, _ := m.paths()
	return filepath.Join(stateDir, "dhclient.leases")
}

func (m *Manager) loadState() (persistedState, error) {
	data, err := m.files().ReadFile(m.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return persistedState{}, nil
		}
		return persistedState{}, fmt.Errorf("wificlient: read state: %w", err)
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return persistedState{}, fmt.Errorf("wificlient: parse state: %w", err)
	}
	return st, nil
}

func (m *Manager) saveState(st persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("wificlient: encode state: %w", err)
	}
	if err := m.files().WriteFile(m.statePath(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("wificlient: write state: %w", err)
	}
	return nil
}

func (m *Manager) prepareInterface(ctx context.Context, iface string) error {
	if _, err := m.runner().CombinedOutput(ctx, "ip", "link", "set", iface, "up"); err != nil {
		return fmt.Errorf("wificlient: ip link set %s up: %w", iface, err)
	}
	return nil
}

func (m *Manager) writeConfig(st persistedState, psk string) error {
	var b strings.Builder
	b.WriteString("ctrl_interface=/run/wpa_supplicant\n")
	b.WriteString("update_config=0\n")
	b.WriteString("network={\n")
	b.WriteString("    ssid=" + quoteWPA(st.SSID) + "\n")
	if strings.TrimSpace(psk) == "" {
		b.WriteString("    key_mgmt=NONE\n")
	} else {
		b.WriteString("    psk=" + quoteWPA(psk) + "\n")
	}
	b.WriteString("}\n")
	if err := m.files().WriteFile(m.confPath(), []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("wificlient: write wpa_supplicant.conf: %w", err)
	}
	return nil
}

func (m *Manager) startServices(ctx context.Context, iface string) error {
	r := m.runner()
	_ = m.teardown(ctx)
	if _, err := r.CombinedOutput(ctx, "wpa_supplicant", "-B", "-i", iface, "-c", m.confPath(), "-P", m.wpaPidPath()); err != nil {
		return fmt.Errorf("wificlient: start wpa_supplicant: %w", err)
	}
	if _, err := r.CombinedOutput(ctx, "dhclient", "-pf", m.dhcpPidPath(), "-lf", m.dhcpLeasePath(), iface); err != nil {
		return fmt.Errorf("wificlient: start dhclient: %w", err)
	}
	return nil
}

func (m *Manager) teardown(ctx context.Context) error {
	r := m.runner()
	var errs []string
	if _, err := r.CombinedOutput(ctx, "pkill", "-f", "wpa_supplicant.*"+m.confPath()); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		if !strings.Contains(strings.ToLower(err.Error()), "no process") {
			errs = append(errs, err.Error())
		}
	}
	if _, err := r.CombinedOutput(ctx, "pkill", "-f", "dhclient.*"+m.dhcpLeasePath()); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		if !strings.Contains(strings.ToLower(err.Error()), "no process") {
			errs = append(errs, err.Error())
		}
	}
	st, _ := m.loadState()
	if st.Iface != "" {
		_, _ = r.CombinedOutput(ctx, "ip", "addr", "flush", "dev", st.Iface)
	}
	if len(errs) > 0 {
		return fmt.Errorf("wificlient: teardown: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) servicesActive(ctx context.Context, iface string) bool {
	r := m.runner()
	wpaOut, err := r.CombinedOutput(ctx, "pgrep", "-f", "wpa_supplicant.*"+m.confPath())
	if err != nil || strings.TrimSpace(wpaOut) == "" {
		return false
	}
	dhcpOut, err := r.CombinedOutput(ctx, "pgrep", "-f", "dhclient.*"+m.dhcpLeasePath())
	return err == nil && strings.TrimSpace(dhcpOut) != ""
}

func packagesPresent(runner wifiap.Runner) bool {
	for _, tool := range []string{"iw", "ip", "wpa_supplicant", "dhclient", "pkill", "pgrep"} {
		if _, err := runner.LookPath(tool); err != nil {
			return false
		}
	}
	return true
}

func quoteWPA(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func defaultSecurity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return SecurityUnknown
	}
	return value
}

func unavailableMessage(reason string) string {
	switch reason {
	case ReasonRadioInUseByAP:
		return "client Wi-Fi cannot start because the available wireless interface is already in Wi-Fi AP mode"
	case ReasonNoHardware:
		return "no client-capable Wi-Fi interface detected"
	default:
		return "client Wi-Fi is unavailable"
	}
}
