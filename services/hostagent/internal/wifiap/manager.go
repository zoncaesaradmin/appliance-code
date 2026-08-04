package wifiap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager owns apply/status for the management WiFi AP.
type Manager struct {
	ConfigDir  string
	StateDir   string
	RuntimeDir string
	Runner     Runner
	Files      FileIO
}

// NewManager returns a production Manager with host defaults.
func NewManager() *Manager {
	return &Manager{
		ConfigDir:  DefaultConfigDir,
		StateDir:   DefaultStateDir,
		RuntimeDir: DefaultRuntimeDir,
		Runner:     ExecRunner{},
		Files:      OSFileIO{},
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

func (m *Manager) runner() Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return ExecRunner{}
}

func (m *Manager) files() FileIO {
	if m.Files != nil {
		return m.Files
	}
	return OSFileIO{}
}

// Status returns desired/actual without secrets.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Desired:           st.Desired,
		Actual:            ActualInactive,
		SSID:              st.SSID,
		Iface:             st.Iface,
		ManagementAddress: ManagementAddress,
		Security:          SecurityWPA2PSK,
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		if st.Desired {
			status.Reason = ReasonPackagesMissing
			status.Message = formatProbeError(ReasonPackagesMissing)
		} else {
			status.Reason = ReasonDesiredOff
			status.Message = "wifi access point is not desired"
		}
		return status, nil
	}
	iface, reason, err := selectInterface(ctx, m.runner())
	if err != nil {
		return Status{}, err
	}
	status.SupportedCapable = reason != ReasonNoHardware && reason != ReasonPackagesMissing
	if st.Iface == "" && iface != "" {
		status.Iface = iface
	}
	if !st.Desired {
		status.Reason = ReasonDesiredOff
		status.Message = "wifi access point is not desired"
		return status, nil
	}
	if reason != ReasonNone {
		status.Reason = reason
		status.Message = formatProbeError(reason)
		return status, nil
	}
	active, activeErr := m.servicesActive(ctx)
	if activeErr != nil {
		status.Actual = ActualFailed
		status.Reason = ReasonServiceStartFailed
		status.Message = activeErr.Error()
		return status, nil
	}
	if active {
		status.Actual = ActualActive
		status.Reason = ReasonNone
		status.Message = "management wifi access point is active"
		return status, nil
	}
	status.Reason = ReasonNotConfigured
	status.Message = "wifi access point is desired but services are not active"
	return status, nil
}

// Apply sets desired state and attempts host configuration when desired.
// Soft hardware/conflict failures leave actual inactive with a reason and return nil error.
func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (Status, error) {
	configDir, stateDir, runtimeDir := m.paths()
	if err := m.files().MkdirAll(configDir, 0o755); err != nil {
		return Status{}, fmt.Errorf("wifiap: create config dir: %w", err)
	}
	if err := m.files().MkdirAll(stateDir, 0o700); err != nil {
		return Status{}, fmt.Errorf("wifiap: create state dir: %w", err)
	}
	if err := m.files().MkdirAll(runtimeDir, 0o755); err != nil {
		return Status{}, fmt.Errorf("wifiap: create runtime dir: %w", err)
	}

	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	st.Desired = req.Desired
	if base := strings.TrimSpace(req.SSIDBase); base != "" {
		st.SSIDBase = base
	}
	if st.SSIDBase == "" {
		st.SSIDBase = "appliance"
	}
	ssid, err := DeriveSSID(st.SSIDBase)
	if err != nil {
		return Status{}, err
	}
	st.SSID = ssid

	if !req.Desired {
		if err := m.teardown(ctx); err != nil {
			// Best-effort: still persist desired=false for status.
			_ = m.saveState(st)
			return Status{}, fmt.Errorf("wifiap: disable: %w", err)
		}
		st.Iface = ""
		if err := m.saveState(st); err != nil {
			return Status{}, err
		}
		_ = m.files().Remove(m.pskPath())
		return m.Status(ctx)
	}

	// Desired on.
	if !packagesPresent(m.runner()) {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonPackagesMissing
		status.Message = formatProbeError(ReasonPackagesMissing)
		return status, nil
	}

	psk := strings.TrimSpace(req.PSK)
	if psk == "" {
		// Reuse previously stored PSK when re-applying without new secret.
		if existing, err := m.loadPSK(); err == nil {
			psk = existing
		}
	}
	if err := ValidatePSK(psk); err != nil {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonPSKMissing
		status.Message = "valid WPA2 PSK is required to activate the access point"
		return status, nil
	}
	if err := m.savePSK(psk); err != nil {
		return Status{}, err
	}

	iface, reason, err := selectInterface(ctx, m.runner())
	if err != nil {
		return Status{}, err
	}
	if reason != ReasonNone {
		st.Iface = ""
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.SSID = st.SSID
		status.Reason = reason
		status.Message = formatProbeError(reason)
		return status, nil
	}
	st.Iface = iface
	if err := m.saveState(st); err != nil {
		return Status{}, err
	}
	if err := m.writeConfigs(st, psk); err != nil {
		return Status{}, err
	}
	if err := m.prepareInterface(ctx, iface); err != nil {
		_ = m.teardown(ctx)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.SSID = st.SSID
		status.Iface = iface
		status.Actual = ActualFailed
		status.Reason = ReasonInterfacePrepare
		status.Message = err.Error()
		return status, nil
	}
	if err := m.startServices(ctx); err != nil {
		_ = m.teardown(ctx)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.SSID = st.SSID
		status.Iface = iface
		status.Actual = ActualFailed
		status.Reason = ReasonServiceStartFailed
		status.Message = err.Error()
		return status, nil
	}
	return m.Status(ctx)
}

func (m *Manager) statePath() string {
	_, stateDir, _ := m.paths()
	return filepath.Join(stateDir, "state.json")
}

func (m *Manager) pskPath() string {
	_, stateDir, _ := m.paths()
	return filepath.Join(stateDir, "psk")
}

func (m *Manager) hostapdConfPath() string {
	configDir, _, _ := m.paths()
	return filepath.Join(configDir, "hostapd.conf")
}

func (m *Manager) dnsmasqConfPath() string {
	configDir, _, _ := m.paths()
	return filepath.Join(configDir, "dnsmasq.conf")
}

func (m *Manager) loadState() (persistedState, error) {
	data, err := m.files().ReadFile(m.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return persistedState{}, nil
		}
		return persistedState{}, fmt.Errorf("wifiap: read state: %w", err)
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return persistedState{}, fmt.Errorf("wifiap: parse state: %w", err)
	}
	return st, nil
}

func (m *Manager) saveState(st persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("wifiap: encode state: %w", err)
	}
	if err := m.files().WriteFile(m.statePath(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("wifiap: write state: %w", err)
	}
	return nil
}

func (m *Manager) loadPSK() (string, error) {
	data, err := m.files().ReadFile(m.pskPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (m *Manager) savePSK(psk string) error {
	if err := m.files().WriteFile(m.pskPath(), []byte(psk+"\n"), 0o600); err != nil {
		return fmt.Errorf("wifiap: write psk: %w", err)
	}
	return nil
}

func (m *Manager) writeConfigs(st persistedState, psk string) error {
	hostapd := fmt.Sprintf(`# Generated by appliance-host-agentd — do not edit
interface=%s
driver=nl80211
ssid=%s
hw_mode=g
channel=6
ieee80211n=1
wmm_enabled=1
auth_algs=1
ignore_broadcast_ssid=0
wpa=2
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
wpa_passphrase=%s
`, st.Iface, st.SSID, psk)
	if err := m.files().WriteFile(m.hostapdConfPath(), []byte(hostapd), 0o600); err != nil {
		return fmt.Errorf("wifiap: write hostapd.conf: %w", err)
	}
	dnsmasq := fmt.Sprintf(`# Generated by appliance-host-agentd — do not edit
interface=%s
bind-interfaces
except-interface=lo
dhcp-range=%s,%s,12h
dhcp-option=3,%s
dhcp-option=6,%s
`, st.Iface, DHCPRangeStart, DHCPRangeEnd, ManagementAddress, ManagementAddress)
	if err := m.files().WriteFile(m.dnsmasqConfPath(), []byte(dnsmasq), 0o644); err != nil {
		return fmt.Errorf("wifiap: write dnsmasq.conf: %w", err)
	}
	return nil
}

func (m *Manager) prepareInterface(ctx context.Context, iface string) error {
	r := m.runner()
	// Bring interface up and assign the fixed management address.
	if _, err := r.CombinedOutput(ctx, "ip", "link", "set", iface, "up"); err != nil {
		return fmt.Errorf("wifiap: ip link set %s up: %w", iface, err)
	}
	// Replace any existing address on the AP path (ignore flush failures).
	_, _ = r.CombinedOutput(ctx, "ip", "addr", "flush", "dev", iface)
	if _, err := r.CombinedOutput(ctx, "ip", "addr", "add", ManagementCIDR, "dev", iface); err != nil {
		// Ignore "File exists" from concurrent run.
		if !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("wifiap: ip addr add %s: %w", ManagementCIDR, err)
		}
	}
	return nil
}

func (m *Manager) startServices(ctx context.Context) error {
	r := m.runner()
	_, _, runtimeDir := m.paths()
	// Stop any previous managed instances first.
	_, _ = r.CombinedOutput(ctx, "pkill", "-f", "hostapd.*"+m.hostapdConfPath())
	_, _ = r.CombinedOutput(ctx, "pkill", "-f", "dnsmasq.*"+m.dnsmasqConfPath())

	if _, err := r.CombinedOutput(ctx, "hostapd", "-B", m.hostapdConfPath()); err != nil {
		return fmt.Errorf("wifiap: start hostapd: %w", err)
	}
	if _, err := r.CombinedOutput(ctx, "dnsmasq", "--conf-file="+m.dnsmasqConfPath(), "--pid-file="+filepath.Join(runtimeDir, "dnsmasq.pid")); err != nil {
		return fmt.Errorf("wifiap: start dnsmasq: %w", err)
	}
	return nil
}

func (m *Manager) teardown(ctx context.Context) error {
	r := m.runner()
	var errs []string
	if _, err := r.CombinedOutput(ctx, "pkill", "-f", "hostapd.*"+m.hostapdConfPath()); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		// pkill returns 1 when no process matched.
		if !strings.Contains(strings.ToLower(err.Error()), "no process") {
			errs = append(errs, err.Error())
		}
	}
	if _, err := r.CombinedOutput(ctx, "pkill", "-f", "dnsmasq.*"+m.dnsmasqConfPath()); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		if !strings.Contains(strings.ToLower(err.Error()), "no process") {
			errs = append(errs, err.Error())
		}
	}
	st, _ := m.loadState()
	if st.Iface != "" {
		_, _ = r.CombinedOutput(ctx, "ip", "addr", "del", ManagementCIDR, "dev", st.Iface)
	}
	if len(errs) > 0 {
		return fmt.Errorf("wifiap: teardown: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) servicesActive(ctx context.Context) (bool, error) {
	r := m.runner()
	hostapdOut, err := r.CombinedOutput(ctx, "pgrep", "-f", "hostapd.*"+m.hostapdConfPath())
	if err != nil || strings.TrimSpace(hostapdOut) == "" {
		return false, nil
	}
	dnsOut, err := r.CombinedOutput(ctx, "pgrep", "-f", "dnsmasq.*"+m.dnsmasqConfPath())
	if err != nil || strings.TrimSpace(dnsOut) == "" {
		return false, nil
	}
	return true, nil
}
