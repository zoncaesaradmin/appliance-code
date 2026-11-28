package mdns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

// Manager owns apply/status for host mDNS (avahi-daemon).
type Manager struct {
	Root     string
	StateDir string
	Runner   Runner
	Files    FileIO
}

// NewManager returns a production Manager with host defaults.
func NewManager() *Manager {
	return &Manager{
		StateDir: DefaultStateDir,
		Runner:   ExecRunner{},
		Files:    OSFileIO{},
	}
}

func (m *Manager) stateDir() string {
	dir := strings.TrimSpace(m.StateDir)
	if dir == "" {
		return DefaultStateDir
	}
	return dir
}

func (m *Manager) root() string {
	root := strings.TrimSpace(m.Root)
	if root == "" {
		return "/"
	}
	return root
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

func (m *Manager) statePath() string {
	return filepath.Join(m.stateDir(), "state.json")
}

// Status returns desired/actual without secrets.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Desired:        st.Desired,
		Actual:         ActualInactive,
		Service:        ServiceName,
		AdvertisedName: applianceMDNSName(st.ApplianceName),
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		if st.Desired {
			status.Reason = ReasonPackagesMissing
			status.Message = "mdns packages (avahi-daemon) are not installed on this host; foundation installation must stage them"
		} else {
			status.Reason = ReasonDesiredOff
			status.Message = "mdns is not desired"
		}
		return status, nil
	}
	status.SupportedCapable = true
	if !st.Desired {
		status.Reason = ReasonDesiredOff
		status.Message = "mdns is not desired"
		return status, nil
	}
	active, activeErr := m.serviceActive(ctx)
	if activeErr != nil {
		status.Actual = ActualFailed
		status.Reason = ReasonServiceStartFailed
		status.Message = activeErr.Error()
		return status, nil
	}
	if active {
		status.Actual = ActualActive
		status.Reason = ReasonNone
		status.Message = "mdns (avahi-daemon) is active"
		return status, nil
	}
	status.Reason = ReasonNotConfigured
	status.Message = "mdns is desired but avahi-daemon is not active"
	return status, nil
}

// Apply sets desired state. Soft package-missing outcomes return status with reason and nil error.
func (m *Manager) Apply(ctx context.Context, req ApplyRequest) (Status, error) {
	if err := m.files().MkdirAll(m.stateDir(), 0o700); err != nil {
		return Status{}, fmt.Errorf("mdns: create state dir: %w", err)
	}
	st, err := m.loadState()
	if err != nil {
		return Status{}, err
	}
	st.Desired = req.Desired
	if requestedName := strings.ToLower(strings.TrimSpace(req.ApplianceName)); requestedName != "" {
		if !validMDNSLabel(requestedName) {
			return Status{}, fmt.Errorf("mdns: appliance name must be a DNS label")
		}
		st.ApplianceName = requestedName
	}
	if !req.Desired {
		if packagesPresent(m.runner()) {
			// Remove only appliance-owned alias publishers. Do not stop or mask
			// avahi-daemon — host names such as gx10.local must keep working.
			if err := m.stopApplianceAliasPublisher(ctx, st.ApplianceName); err != nil {
				_ = m.saveState(st)
				return Status{}, fmt.Errorf("mdns: disable: %w", err)
			}
		}
		if err := m.saveState(st); err != nil {
			return Status{}, err
		}
		return m.Status(ctx)
	}

	// Desired on.
	if !packagesPresent(m.runner()) {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonPackagesMissing
		status.Message = "mdns packages (avahi-daemon) are not installed on this host; foundation installation must stage them"
		return status, nil
	}
	if st.ApplianceName == "" {
		_ = m.saveState(st)
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Reason = ReasonNotConfigured
		status.Message = "mdns requires the appliance name configured during installation"
		return status, nil
	}
	// Bind Avahi to the LAN iface when needed, but never replace the host's
	// Avahi host-name (gx10.local / vendor names). Appliance identity is an
	// added <name>.local publisher alongside the existing host name.
	if err := m.configureAvahiLANBinding(ctx); err != nil {
		return Status{}, err
	}
	if err := m.clearStaleApplianceHostNameOverride(ctx, st.ApplianceName); err != nil {
		return Status{}, err
	}
	if err := m.saveState(st); err != nil {
		return Status{}, err
	}
	if err := m.startService(ctx); err != nil {
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Actual = ActualFailed
		status.Reason = ReasonServiceStartFailed
		status.Message = err.Error()
		return status, nil
	}
	if err := m.ensureApplianceAliasPublisher(ctx, st.ApplianceName); err != nil {
		status, _ := m.Status(ctx)
		status.Desired = true
		status.Actual = ActualFailed
		status.Reason = ReasonServiceStartFailed
		status.Message = err.Error()
		return status, nil
	}
	return m.Status(ctx)
}

// configureAvahiLANBinding limits Avahi to the default-route LAN interface so
// K3s veth/CNI interfaces do not create isolated mDNS domains. It never sets
// host-name — the system / vendor Avahi host name (e.g. gx10.local) stays as
// the primary advertisement; the appliance adds <name>.local separately.
func (m *Manager) configureAvahiLANBinding(ctx context.Context) error {
	iface, err := m.defaultRouteInterface(ctx)
	if err != nil {
		return err
	}
	path := m.avahiConfigPath(ctx)
	data, err := m.files().ReadFile(path)
	if err != nil {
		return fmt.Errorf("mdns: read avahi configuration %s: %w", path, err)
	}
	updated, err := setServerOption(string(data), "allow-interfaces", iface)
	if err != nil {
		return err
	}
	if updated == string(data) {
		return nil
	}
	if err := m.files().WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("mdns: write avahi configuration %s: %w", path, err)
	}
	return nil
}

// clearStaleApplianceHostNameOverride removes a prior appliance install's
// host-name=<appliance> overwrite so Avahi can fall back to the system
// hostname again. Vendor host-name values (nvidia aibox, etc.) are left alone.
func (m *Manager) clearStaleApplianceHostNameOverride(ctx context.Context, applianceName string) error {
	applianceName = strings.ToLower(strings.TrimSpace(applianceName))
	if applianceName == "" {
		return nil
	}
	paths := map[string]struct{}{
		m.avahiConfigPath(ctx): {},
		filepath.Join(m.root(), "etc", "avahi", "avahi-daemon.conf"): {},
	}
	for path := range paths {
		data, err := m.files().ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("mdns: read %s: %w", path, err)
		}
		updated, changed := removeServerOptionValue(string(data), "host-name", applianceName)
		if !changed {
			continue
		}
		if err := m.files().WriteFile(path, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("mdns: restore host-name in %s: %w", path, err)
		}
	}
	return nil
}

func removeServerOptionValue(config, key, value string) (string, bool) {
	want := key + "=" + value
	lines := strings.Split(config, "\n")
	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == want {
			changed = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed
}

func (m *Manager) applianceAliasPublisherFile(applianceName string) string {
	return filepath.Join(m.root(), "etc", "systemd", "system", applianceAliasPublisherUnit(applianceName))
}

func applianceAliasPublisherUnit(applianceName string) string {
	return "zon-mdns-appliance-" + strings.ToLower(strings.TrimSpace(applianceName)) + ".service"
}

func (m *Manager) ensureApplianceAliasPublisher(ctx context.Context, applianceName string) error {
	alias := applianceMDNSName(applianceName)
	if alias == "" {
		return fmt.Errorf("mdns: invalid appliance name %q", applianceName)
	}
	if _, err := m.runner().LookPath("avahi-publish-address"); err != nil {
		return fmt.Errorf("mdns: publish appliance alias: avahi-publish-address is unavailable: %w", err)
	}
	address, err := m.primaryAddress(ctx)
	if err != nil {
		return err
	}
	unitPath := m.applianceAliasPublisherFile(applianceName)
	unit := "[Unit]\nDescription=Zon appliance mDNS name " + alias + "\nRequires=avahi-daemon.service\nAfter=avahi-daemon.service\n\n[Service]\nType=simple\nExecStart=/usr/bin/avahi-publish-address -R " + alias + " " + address + "\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=multi-user.target\n"
	if err := m.files().MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("mdns: create appliance alias publisher directory: %w", err)
	}
	existing, readErr := m.files().ReadFile(unitPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("mdns: read appliance alias publisher: %w", readErr)
	}
	changed := readErr != nil || string(existing) != unit
	if changed {
		if err := m.files().WriteFile(unitPath, []byte(unit), 0o644); err != nil {
			return fmt.Errorf("mdns: write appliance alias publisher: %w", err)
		}
		if _, err := m.runner().CombinedOutput(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("mdns: reload appliance alias publisher: %w", err)
		}
	}
	unitName := applianceAliasPublisherUnit(applianceName)
	if _, err := m.runner().CombinedOutput(ctx, "systemctl", "enable", "--now", unitName); err != nil {
		return fmt.Errorf("mdns: start appliance alias publisher %q: %w", alias, err)
	}
	if changed && readErr == nil {
		// enable --now leaves an already-running unit on its old ExecStart.
		// Restart only when its published address actually changed.
		if _, err := m.runner().CombinedOutput(ctx, "systemctl", "restart", unitName); err != nil {
			return fmt.Errorf("mdns: restart appliance alias publisher %q: %w", alias, err)
		}
	}
	return nil
}

func (m *Manager) stopApplianceAliasPublisher(ctx context.Context, applianceName string) error {
	applianceName = strings.ToLower(strings.TrimSpace(applianceName))
	if applianceName == "" {
		return nil
	}
	unitName := applianceAliasPublisherUnit(applianceName)
	_, _ = m.runner().CombinedOutput(ctx, "systemctl", "disable", "--now", unitName)
	if err := m.files().Remove(m.applianceAliasPublisherFile(applianceName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mdns: remove appliance alias publisher: %w", err)
	}
	_, _ = m.runner().CombinedOutput(ctx, "systemctl", "daemon-reload")
	return nil
}

// avahiConfigPath returns the config file the running unit actually loads.
// Vendor packages may override ExecStart with -f /path; patch that file so we
// do not silently edit an unused avahi-daemon.conf.
func (m *Manager) avahiConfigPath(ctx context.Context) string {
	defaultPath := filepath.Join(m.root(), "etc", "avahi", "avahi-daemon.conf")
	out, err := m.runner().CombinedOutput(ctx, "systemctl", "show", "-p", "ExecStart", "--value", ServiceName)
	if err != nil {
		return defaultPath
	}
	fields := strings.Fields(out)
	for index, field := range fields {
		if field != "-f" || index+1 >= len(fields) {
			continue
		}
		cfg := fields[index+1]
		if !filepath.IsAbs(cfg) {
			continue
		}
		if m.root() == "/" || m.root() == "" {
			return cfg
		}
		return filepath.Join(m.root(), strings.TrimPrefix(cfg, "/"))
	}
	return defaultPath
}

func (m *Manager) defaultRouteInterface(ctx context.Context) (string, error) {
	out, err := m.runner().CombinedOutput(ctx, "ip", "-4", "route", "show", "default")
	if err != nil {
		return "", fmt.Errorf("mdns: find default route: %w", err)
	}
	fields := strings.Fields(out)
	for index, field := range fields {
		if index+1 >= len(fields) {
			break
		}
		if field == "dev" && validInterfaceName(fields[index+1]) {
			return fields[index+1], nil
		}
	}
	return "", fmt.Errorf("mdns: no usable IPv4 default-route interface")
}

func validInterfaceName(value string) bool {
	if value == "" || len(value) > 15 || value == "lo" {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func setServerOption(config, key, value string) (string, error) {
	lines := strings.Split(config, "\n")
	server := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "[server]" {
			server = index
			break
		}
	}
	if server < 0 {
		return "", fmt.Errorf("mdns: avahi configuration has no [server] section")
	}
	for index := server + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			break
		}
		if strings.HasPrefix(trimmed, key+"=") {
			lines[index] = key + "=" + value
			return strings.Join(lines, "\n"), nil
		}
	}
	lines = append(lines[:server+1], append([]string{key + "=" + value}, lines[server+1:]...)...)
	return strings.Join(lines, "\n"), nil
}

func (m *Manager) primaryAddress(ctx context.Context) (string, error) {
	iface, err := m.defaultRouteInterface(ctx)
	if err != nil {
		return "", err
	}
	out, err := m.runner().CombinedOutput(ctx, "ip", "-4", "-o", "addr", "show", "dev", iface, "scope", "global")
	if err != nil {
		return "", fmt.Errorf("mdns: find LAN address on %s: %w", iface, err)
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for index, field := range fields {
			if field != "inet" || index+1 >= len(fields) {
				continue
			}
			prefix, err := netip.ParsePrefix(fields[index+1])
			if err != nil {
				continue
			}
			addr := prefix.Addr()
			if addr.Is4() && !addr.IsUnspecified() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsMulticast() {
				return addr.String(), nil
			}
		}
	}
	return "", fmt.Errorf("mdns: no usable LAN IPv4 address on default-route interface %s", iface)
}

func validMDNSLabel(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || (char == '-' && index > 0 && index < len(value)-1) {
			continue
		}
		return false
	}
	return true
}

func applianceMDNSName(applianceName string) string {
	applianceName = strings.ToLower(strings.TrimSpace(applianceName))
	if !validMDNSLabel(applianceName) {
		return ""
	}
	return applianceName + ".local"
}

// Reconcile restores avahi-daemon when desired state survived a reboot but the
// unit is not active. Prefer systemd enablement from Apply; this covers drift.
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
		if st.ApplianceName != "" {
			if err := m.ensureApplianceAliasPublisher(ctx, st.ApplianceName); err != nil {
				return status, err
			}
		}
		return status, nil
	}
	return m.Apply(ctx, ApplyRequest{Desired: true, ApplianceName: st.ApplianceName})
}

func packagesPresent(r Runner) bool {
	if _, err := r.LookPath("avahi-daemon"); err != nil {
		return false
	}
	if _, err := r.LookPath("systemctl"); err != nil {
		return false
	}
	return true
}

func (m *Manager) serviceActive(ctx context.Context) (bool, error) {
	out, err := m.runner().CombinedOutput(ctx, "systemctl", "is-active", ServiceName)
	if err != nil {
		// systemctl returns non-zero for inactive/failed units.
		if strings.Contains(out, "failed") {
			return false, fmt.Errorf("%s is failed", ServiceName)
		}
		if strings.Contains(out, "inactive") || strings.Contains(out, "dead") {
			return false, nil
		}
		// Missing unit treated as inactive.
		if strings.Contains(strings.ToLower(err.Error()), "could not be found") ||
			strings.Contains(strings.ToLower(err.Error()), "not-found") {
			return false, nil
		}
		// is-active often exits 3 for inactive — treat clean inactive as not active.
		if strings.TrimSpace(out) == "inactive" || strings.TrimSpace(out) == "failed" {
			return false, nil
		}
		// Default: inactive without hard error if output is empty/inactive.
		if strings.TrimSpace(out) != "active" {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) == "active", nil
}

func (m *Manager) startService(ctx context.Context) error {
	r := m.runner()
	// Avahi socket+service are interdependent (Requires=). Unmask both, then
	// start only if inactive — never stop/mask mid-enable (cancels jobs and
	// drops host vendor mDNS state). Existing /etc/avahi/services entries and
	// vendor drop-ins stay in place; configureAvahiServer only patches options.
	for _, unit := range []string{SocketName, ServiceName} {
		if _, err := r.CombinedOutput(ctx, "systemctl", "unmask", unit); err != nil {
			_ = err
		}
	}
	active, err := m.serviceActive(ctx)
	if err != nil {
		return err
	}
	if _, err := r.CombinedOutput(ctx, "systemctl", "enable", ServiceName); err != nil {
		return fmt.Errorf("mdns: enable %s: %w", ServiceName, err)
	}
	if active {
		// Config may have changed (host-name / allow-interfaces); reload in place.
		if _, err := r.CombinedOutput(ctx, "systemctl", "reload-or-restart", ServiceName); err != nil {
			return fmt.Errorf("mdns: reload-or-restart %s: %w", ServiceName, err)
		}
		return nil
	}
	if _, err := r.CombinedOutput(ctx, "systemctl", "start", ServiceName); err != nil {
		// Canceled races: accept if the unit became active anyway.
		if activeNow, activeErr := m.serviceActive(ctx); activeErr == nil && activeNow {
			return nil
		}
		return fmt.Errorf("mdns: start %s: %w", ServiceName, err)
	}
	return nil
}

func (m *Manager) stopService(ctx context.Context) error {
	r := m.runner()
	// Stop socket+service in one replace job so activation cannot cancel a peer.
	_, _ = r.CombinedOutput(ctx, "systemctl", "stop", "--job-mode=replace", SocketName, ServiceName)
	for _, unit := range []string{SocketName, ServiceName} {
		_, _ = r.CombinedOutput(ctx, "systemctl", "disable", unit)
		_, _ = r.CombinedOutput(ctx, "systemctl", "mask", unit)
	}
	return nil
}

func (m *Manager) loadState() (persistedState, error) {
	data, err := m.files().ReadFile(m.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return persistedState{}, nil
		}
		return persistedState{}, fmt.Errorf("mdns: read state: %w", err)
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return persistedState{}, fmt.Errorf("mdns: parse state: %w", err)
	}
	return st, nil
}

func (m *Manager) saveState(st persistedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("mdns: encode state: %w", err)
	}
	if err := m.files().WriteFile(m.statePath(), data, 0o600); err != nil {
		return fmt.Errorf("mdns: write state: %w", err)
	}
	return nil
}
