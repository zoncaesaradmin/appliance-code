package mdns

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager owns apply/status for host mDNS (avahi-daemon).
type Manager struct {
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
		Desired: st.Desired,
		Actual:  ActualInactive,
		Service: ServiceName,
	}
	if !packagesPresent(m.runner()) {
		status.SupportedCapable = false
		if st.Desired {
			status.Reason = ReasonPackagesMissing
			status.Message = "mdns packages (avahi-daemon) are not installed on this host; reinstall with host mDNS packages enabled"
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
	st := persistedState{Desired: req.Desired}
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
		status.Message = "mdns packages (avahi-daemon) are not installed on this host; reinstall with host mDNS packages enabled"
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
