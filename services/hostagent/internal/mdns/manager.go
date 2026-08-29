package mdns

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"appliance-code/services/hostagent/internal/host"
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
		AdvertisedName: host.MDNSAdvertisedName(m.root()),
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		if st.Desired {
			status.Reason = ReasonPackagesMissing
			status.Message = "mdns packages (avahi-daemon) are not installed on this host; complete product install stages host packages for day-2 enablement"
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
	if !req.Desired {
		if packagesPresent(m.runner()) {
			if err := m.stopService(ctx); err != nil {
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
		status.Message = "mdns packages (avahi-daemon) are not installed on this host; complete product install stages host packages for day-2 enablement"
		return status, nil
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
	return m.Status(ctx)
}

// ApplyApplicationServices writes the small, validated Avahi service files
// owned by one catalog application and enables mDNS when services are present.
func (m *Manager) ApplyApplicationServices(ctx context.Context, req ApplicationRequest) error {
	if !validApplicationName(req.Application) {
		return fmt.Errorf("mdns: application must be a DNS label")
	}
	for _, service := range req.Services {
		if !validApplicationService(service) {
			return fmt.Errorf("mdns: application service must use a DNS-label name, valid service type, and port")
		}
	}
	for _, alias := range req.Aliases {
		if !validApplicationAlias(alias) {
			return fmt.Errorf("mdns: application alias must be a single .local hostname")
		}
	}
	st, err := m.loadState()
	if err != nil {
		return err
	}
	if st.ApplicationServices == nil {
		st.ApplicationServices = map[string][]ApplicationService{}
	}
	if st.ApplicationAliases == nil {
		st.ApplicationAliases = map[string][]string{}
	}
	requestedServices := normalizedServices(req.Services)
	requestedAliases := uniqueSorted(req.Aliases)
	if st.Desired && sameApplicationServices(st.ApplicationServices[req.Application], requestedServices) &&
		strings.Join(uniqueSorted(st.ApplicationAliases[req.Application]), ",") == strings.Join(requestedAliases, ",") {
		// Application reconciliation runs repeatedly. Rewriting identical Avahi
		// files would restart the daemon on every pass, preventing mDNS probing
		// and publication from ever settling.
		return m.verifyApplicationAliases(ctx, requestedAliases)
	}
	if err := m.removeApplicationFiles(req.Application, st.ApplicationServices[req.Application]); err != nil {
		return err
	}
	if err := m.removeApplicationAliasPublishers(ctx, req.Application, st.ApplicationAliases[req.Application]); err != nil {
		return err
	}
	if len(req.Services) == 0 {
		delete(st.ApplicationServices, req.Application)
	} else {
		st.ApplicationServices[req.Application] = requestedServices
		if err := m.writeApplicationFiles(req.Application, req.Services); err != nil {
			return err
		}
		st.Desired = true
	}
	if len(req.Aliases) == 0 {
		delete(st.ApplicationAliases, req.Application)
	} else {
		st.ApplicationAliases[req.Application] = requestedAliases
		st.Desired = true
	}
	if err := m.writeApplicationAliases(ctx, st); err != nil {
		return err
	}
	if err := m.saveState(st); err != nil {
		return err
	}
	if len(req.Services) > 0 || len(req.Aliases) > 0 {
		_, err := m.Apply(ctx, ApplyRequest{Desired: true})
		if err != nil {
			return err
		}
		if err := m.startApplicationAliasPublishers(ctx, req.Application, requestedAliases); err != nil {
			return err
		}
		return m.verifyApplicationAliases(ctx, requestedAliases)
	}
	// Avahi observes hosts files on reload, but a restart gives the same
	// deterministic convergence used for newly created service records.
	if st.Desired && packagesPresent(m.runner()) {
		if err := m.startService(ctx); err != nil {
			return err
		}
	}
	return nil
}

// verifyApplicationAliases verifies the records clients will use, not merely
// that the Avahi process started. A daemon may remain active after rejecting a
// static alias because of an mDNS name collision.
func (m *Manager) verifyApplicationAliases(ctx context.Context, aliases []string) error {
	if len(aliases) == 0 {
		return nil
	}
	if _, err := m.runner().LookPath("avahi-resolve-host-name"); err != nil {
		return fmt.Errorf("mdns: verify application aliases: avahi-resolve-host-name is unavailable: %w", err)
	}
	for _, alias := range aliases {
		if _, err := m.runner().CombinedOutput(ctx, "avahi-resolve-host-name", "-4", alias); err != nil {
			return fmt.Errorf("mdns: application alias %q was not published: %w", alias, err)
		}
	}
	return nil
}

func (m *Manager) applicationServicesDir() string {
	return filepath.Join(m.root(), "etc", "avahi", "services")
}

func (m *Manager) applicationFile(application, name string) string {
	return filepath.Join(m.applicationServicesDir(), "zon-"+application+"-"+name+".service")
}

func (m *Manager) applicationAliasesFile() string {
	return filepath.Join(m.root(), "etc", "avahi", "hosts")
}

func (m *Manager) applicationAliasPublisherFile(application, alias string) string {
	return filepath.Join(m.root(), "etc", "systemd", "system", "zon-mdns-"+application+"-"+alias+".service")
}

func applicationAliasPublisherUnit(application, alias string) string {
	return "zon-mdns-" + application + "-" + alias + ".service"
}

const applicationAliasBegin = "# BEGIN ZON APPLICATION ALIASES"
const applicationAliasEnd = "# END ZON APPLICATION ALIASES"

// configureApplicationInterfaces keeps application aliases on the real LAN.
// Avahi otherwise joins every K3s veth/CNI interface, creating isolated mDNS
// domains that can collide with an appliance-owned alias.
func (m *Manager) configureApplicationInterfaces(ctx context.Context) error {
	iface, err := m.defaultRouteInterface(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(m.root(), "etc", "avahi", "avahi-daemon.conf")
	data, err := m.files().ReadFile(path)
	if err != nil {
		return fmt.Errorf("mdns: read avahi configuration: %w", err)
	}
	updated, err := setServerOption(string(data), "allow-interfaces", iface)
	if err != nil {
		return err
	}
	if updated == string(data) {
		return nil
	}
	if err := m.files().WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("mdns: write avahi configuration: %w", err)
	}
	return nil
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

// writeApplicationAliases removes the former static-host block and writes one
// supervised Avahi address publisher per application alias. Static hosts are
// registered inside avahi-daemon and can be rejected locally before they ever
// reach the LAN; publisher clients support independent alias entry groups.
func (m *Manager) writeApplicationAliases(ctx context.Context, st persistedState) error {
	path := m.applicationAliasesFile()
	existing, err := m.files().ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mdns: read application aliases: %w", err)
	}
	base := removeAliasBlock(string(existing))
	if strings.TrimSpace(base) != strings.TrimSpace(string(existing)) {
		if err := m.files().WriteFile(path, []byte(base), 0o644); err != nil {
			return fmt.Errorf("mdns: remove legacy application aliases: %w", err)
		}
	}
	if len(allAliases(st.ApplicationAliases)) == 0 {
		return nil
	}
	if err := m.configureApplicationInterfaces(ctx); err != nil {
		return err
	}
	address, err := m.primaryAddress(ctx)
	if err != nil {
		return err
	}
	for application, aliases := range st.ApplicationAliases {
		for _, alias := range uniqueSorted(aliases) {
			// The appliance hostname already owns the address's reverse record.
			// Publish only this application alias's forward A record to avoid a
			// self-conflict between jellyfin.local and zonsyssrv1.local.
			unit := "[Unit]\nDescription=Zon mDNS address publisher for " + alias + "\nRequires=avahi-daemon.service\nAfter=avahi-daemon.service\n\n[Service]\nType=simple\nExecStart=/usr/bin/avahi-publish-address -R " + alias + " " + address + "\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=multi-user.target\n"
			if err := m.files().MkdirAll(filepath.Dir(m.applicationAliasPublisherFile(application, alias)), 0o755); err != nil {
				return fmt.Errorf("mdns: create alias publisher directory: %w", err)
			}
			if err := m.files().WriteFile(m.applicationAliasPublisherFile(application, alias), []byte(unit), 0o644); err != nil {
				return fmt.Errorf("mdns: write alias publisher: %w", err)
			}
		}
	}
	return nil
}

func (m *Manager) removeApplicationAliasPublishers(ctx context.Context, application string, aliases []string) error {
	for _, alias := range uniqueSorted(aliases) {
		unit := applicationAliasPublisherUnit(application, alias)
		_, _ = m.runner().CombinedOutput(ctx, "systemctl", "disable", "--now", unit)
		if err := m.files().Remove(m.applicationAliasPublisherFile(application, alias)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("mdns: remove alias publisher: %w", err)
		}
	}
	return nil
}

func (m *Manager) startApplicationAliasPublishers(ctx context.Context, application string, aliases []string) error {
	if len(aliases) == 0 {
		return nil
	}
	if _, err := m.runner().LookPath("avahi-publish-address"); err != nil {
		return fmt.Errorf("mdns: publish application aliases: avahi-publish-address is unavailable: %w", err)
	}
	if _, err := m.runner().CombinedOutput(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("mdns: reload alias publishers: %w", err)
	}
	for _, alias := range uniqueSorted(aliases) {
		unit := applicationAliasPublisherUnit(application, alias)
		if _, err := m.runner().CombinedOutput(ctx, "systemctl", "enable", "--now", unit); err != nil {
			return fmt.Errorf("mdns: start alias publisher %q: %w", alias, err)
		}
	}
	return nil
}

func (m *Manager) primaryAddress(ctx context.Context) (string, error) {
	out, err := m.runner().CombinedOutput(ctx, "hostname", "-I")
	if err != nil {
		return "", fmt.Errorf("mdns: find host address: %w", err)
	}
	for _, value := range strings.Fields(out) {
		// Avahi's static host file accepts IPv4/IPv6, but publish the ordinary
		// LAN IPv4 address only. Loopback, link-local, and management AP space
		// must not make an application reachable from the wrong network.
		parts := strings.Split(value, ".")
		if len(parts) != 4 || value == "127.0.0.1" || strings.HasPrefix(value, "169.254.") || strings.HasPrefix(value, "10.42.0.") {
			continue
		}
		return value, nil
	}
	return "", fmt.Errorf("mdns: no usable LAN IPv4 address is available")
}

func removeAliasBlock(content string) string {
	start := strings.Index(content, applicationAliasBegin)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], applicationAliasEnd)
	if end < 0 {
		return content[:start]
	}
	end += start + len(applicationAliasEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:start] + content[end:]
}

func allAliases(byApplication map[string][]string) []string {
	var aliases []string
	for _, values := range byApplication {
		aliases = append(aliases, values...)
	}
	return uniqueSorted(aliases)
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedServices(values []ApplicationService) []ApplicationService {
	result := append([]ApplicationService(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].ServiceType != result[j].ServiceType {
			return result[i].ServiceType < result[j].ServiceType
		}
		return result[i].Port < result[j].Port
	})
	return result
}

func sameApplicationServices(left, right []ApplicationService) bool {
	left = normalizedServices(left)
	right = normalizedServices(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (m *Manager) removeApplicationFiles(application string, services []ApplicationService) error {
	for _, service := range services {
		if err := m.files().Remove(m.applicationFile(application, service.Name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("mdns: remove application service: %w", err)
		}
	}
	return nil
}

func (m *Manager) writeApplicationFiles(application string, services []ApplicationService) error {
	if err := m.files().MkdirAll(m.applicationServicesDir(), 0o755); err != nil {
		return fmt.Errorf("mdns: create application services directory: %w", err)
	}
	sorted := append([]ApplicationService(nil), services...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, service := range sorted {
		content := fmt.Sprintf("<service-group>\n  <name replace-wildcards=\"yes\">%s</name>\n  <service>\n    <type>%s</type>\n    <port>%d</port>\n  </service>\n</service-group>\n", service.Name, service.ServiceType, service.Port)
		if err := m.files().WriteFile(m.applicationFile(application, service.Name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("mdns: write application service: %w", err)
		}
	}
	return nil
}

func validApplicationName(value string) bool { return validMDNSLabel(value) }

func validApplicationService(service ApplicationService) bool {
	if !validMDNSLabel(service.Name) || service.Port < 1 || service.Port > 65535 || !strings.HasPrefix(service.ServiceType, "_") || len(service.ServiceType) > 80 {
		return false
	}
	for _, char := range service.ServiceType {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
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

func validApplicationAlias(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasSuffix(value, ".local") {
		return false
	}
	return validMDNSLabel(strings.TrimSuffix(value, ".local"))
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
		return status, nil
	}
	return m.Apply(ctx, ApplyRequest{Desired: true})
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
		if strings.Contains(out, "inactive") || strings.Contains(out, "failed") || strings.Contains(out, "dead") {
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
	if _, err := r.CombinedOutput(ctx, "systemctl", "unmask", ServiceName); err != nil {
		// unmask is best-effort if never masked.
		_ = err
	}
	if _, err := r.CombinedOutput(ctx, "systemctl", "enable", ServiceName); err != nil {
		return fmt.Errorf("mdns: enable %s: %w", ServiceName, err)
	}
	if _, err := r.CombinedOutput(ctx, "systemctl", "restart", ServiceName); err != nil {
		return fmt.Errorf("mdns: restart %s: %w", ServiceName, err)
	}
	return nil
}

func (m *Manager) stopService(ctx context.Context) error {
	r := m.runner()
	_, _ = r.CombinedOutput(ctx, "systemctl", "stop", ServiceName)
	_, _ = r.CombinedOutput(ctx, "systemctl", "disable", ServiceName)
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
