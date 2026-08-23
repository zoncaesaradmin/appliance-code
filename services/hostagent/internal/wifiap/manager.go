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
	PortBinder PortBindProber
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
		Desired:            st.Desired,
		Actual:             ActualInactive,
		SSID:               st.SSID,
		Iface:              st.Iface,
		ManagementAddress:  ManagementAddress,
		ManagementHostname: ManagementHostname,
		ManagementURL:      ManagementURL(),
		Security:           SecurityWPA2PSK,
	}
	if st.Desired {
		local := st.LocalDNSServing
		status.LocalDNSServing = &local
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		status.CapabilityState = "unknown"
		status.CapabilityDetail = "AP hardware capability cannot be assessed because the required host packages are not installed."
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
	switch reason {
	case ReasonNoHardware:
		status.CapabilityState = "unsupported"
		status.CapabilityDetail = "The host did not detect an AP-capable Wi-Fi interface."
	case ReasonPackagesMissing:
		status.CapabilityState = "unknown"
		status.CapabilityDetail = "AP hardware capability cannot be assessed because the required host packages are not installed."
	default:
		status.CapabilityState = "supported"
		status.CapabilityDetail = "An AP-capable Wi-Fi interface is detected."
	}
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
		if st.LocalDNSServing {
			status.Message = "management wifi access point is active; open " + ManagementURL() + " (local DNS for " + ManagementHostname + ")"
		} else {
			status.Message = "management wifi access point is active; open " + ManagementURL() + " or https://" + ManagementAddress + "/ (host DNS owns :53)"
		}
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
		if host, herr := os.Hostname(); herr == nil && strings.TrimSpace(host) != "" {
			st.SSIDBase = strings.TrimSpace(host)
		} else {
			st.SSIDBase = "appliance"
		}
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
		st.LocalDNSServing = false
		if err := m.saveState(st); err != nil {
			return Status{}, err
		}
		// Wipe secrets and generated runtime config so disable is a full cleanup.
		_ = m.files().Remove(m.pskPath())
		_ = m.files().Remove(m.hostapdConfPath())
		_ = m.files().Remove(m.dnsmasqConfPath())
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
		// Reuse the stored passphrase only when re-activating an already-configured
		// AP (boot reconcile / operator retry). Fresh enable still requires an
		// explicit PSK; disable wipes the stored secret.
		stored, loadErr := m.loadPSK()
		if loadErr == nil {
			psk = strings.TrimSpace(stored)
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
	return m.bringUp(ctx, st, psk, bringUpOpts{})
}

// Reconcile restores the management AP when desired state survived a reboot but
// hostapd/dnsmasq did not. No-op when desired is off or services are already active.
//
// Boot restore always uses DHCP-only dnsmasq (port=0). A free-port probe right
// after reboot would often succeed before product CoreDNS (hostNetwork :53) is
// up, and AP dnsmasq on 10.42.0.1:53 would then block CoreDNS from binding *:53.
// manage.ap stays on CoreDNS when present; otherwise use https://10.42.0.1/.
func (m *Manager) Reconcile(ctx context.Context) (Status, error) {
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	if !st.Desired {
		return m.Status(ctx)
	}
	status, err := m.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if status.Actual == ActualActive {
		return status, nil
	}
	psk, err := m.loadPSK()
	if err != nil || ValidatePSK(strings.TrimSpace(psk)) != nil {
		return status, nil
	}
	psk = strings.TrimSpace(psk)
	if st.SSID == "" {
		ssid, derr := DeriveSSID(st.SSIDBase)
		if derr != nil {
			return Status{}, derr
		}
		st.SSID = ssid
	}
	return m.bringUp(ctx, st, psk, bringUpOpts{forceDHCPOnly: true})
}

type bringUpOpts struct {
	forceDHCPOnly bool
}

func (m *Manager) bringUp(ctx context.Context, st persistedState, psk string, opts bringUpOpts) (Status, error) {
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
	// Assign management IP first so the :53 bind probe targets a real local address.
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
	// Socket ownership only (not "is CoreDNS installed?"): free → dnsmasq DNS;
	// busy → DHCP-only and rely on host DNS that holds manage.ap (CoreDNS).
	// Boot reconcile forces DHCP-only so we never race product CoreDNS.
	if opts.forceDHCPOnly {
		st.LocalDNSServing = false
	} else {
		st.LocalDNSServing = m.portBinder().CanBindManagementDNS()
	}
	if err := m.saveState(st); err != nil {
		return Status{}, err
	}
	if err := m.writeConfigs(st, psk); err != nil {
		return Status{}, err
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
	dnsmasq := renderDnsmasqConf(st.Iface, st.LocalDNSServing)
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
