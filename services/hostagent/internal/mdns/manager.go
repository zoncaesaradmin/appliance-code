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
	st, err := m.loadState()
	if err != nil {
		return err
	}
	if st.ApplicationServices == nil {
		st.ApplicationServices = map[string][]ApplicationService{}
	}
	if err := m.removeApplicationFiles(req.Application, st.ApplicationServices[req.Application]); err != nil {
		return err
	}
	if len(req.Services) == 0 {
		delete(st.ApplicationServices, req.Application)
	} else {
		st.ApplicationServices[req.Application] = append([]ApplicationService(nil), req.Services...)
		if err := m.writeApplicationFiles(req.Application, req.Services); err != nil {
			return err
		}
		st.Desired = true
	}
	if err := m.saveState(st); err != nil {
		return err
	}
	if len(req.Services) > 0 {
		_, err := m.Apply(ctx, ApplyRequest{Desired: true})
		return err
	}
	return nil
}

func (m *Manager) applicationServicesDir() string {
	return filepath.Join(m.root(), "etc", "avahi", "services")
}

func (m *Manager) applicationFile(application, name string) string {
	return filepath.Join(m.applicationServicesDir(), "zon-"+application+"-"+name+".service")
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
