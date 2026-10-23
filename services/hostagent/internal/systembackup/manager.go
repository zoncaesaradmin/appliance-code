// Package systembackup owns the small allowlisted host operation used by the
// appliance API. It intentionally executes only zonctl's named backup command;
// it is not a general command bridge.
package systembackup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const DefaultRoot = "/data/zon/backups"
const MaxArchiveBytes int64 = 64 << 20

type Status struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Message         string    `json:"message,omitempty"`
	RecoverySetPath string    `json:"recoverySetPath,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
type Manager struct {
	Root, Zonctl string
	mu           sync.RWMutex
	operations   map[string]Status
}

func NewManager(root, zonctl string) *Manager {
	if root == "" {
		root = DefaultRoot
	}
	if zonctl == "" {
		zonctl = "/usr/local/lib/zon/bin/zonctl-real"
	}
	return &Manager{Root: root, Zonctl: zonctl, operations: map[string]Status{}}
}
func (m *Manager) Create(ctx context.Context, archive []byte) (Status, error) {
	if len(archive) == 0 || len(archive) > 64<<20 {
		return Status{}, fmt.Errorf("system backup archive is required and must be at most 64 MiB")
	}
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return Status{}, err
	}
	idb := make([]byte, 8)
	if _, err := rand.Read(idb); err != nil {
		return Status{}, err
	}
	id := "recovery-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(idb)
	path := filepath.Join(m.Root, id)
	staged := filepath.Join(m.Root, "."+id+".zonconfig")
	if err := os.WriteFile(staged, archive, 0600); err != nil {
		return Status{}, err
	}
	now := time.Now().UTC()
	s := Status{ID: id, Status: "running", RecoverySetPath: path, CreatedAt: now, UpdatedAt: now}
	m.put(s)
	go m.run(id, path, staged)
	return s, nil
}
func (m *Manager) run(id, path, staged string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	defer os.Remove(staged)
	out, err := exec.CommandContext(ctx, m.Zonctl, "backup", "--recovery-set", path, "--prepared-configuration-archive", staged, "--output", "json").CombinedOutput()
	m.mu.Lock()
	s := m.operations[id]
	s.UpdatedAt = time.Now().UTC()
	if err != nil {
		s.Status = "failed"
		s.Message = fmt.Sprintf("backup failed: %v", err)
	} else {
		s.Status = "succeeded"
		s.Message = "verified system backup created"
	}
	_ = out
	m.operations[id] = s
	m.mu.Unlock()
}
func (m *Manager) Get(_ context.Context, id string) (Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.operations[id]
	if !ok {
		return Status{}, fmt.Errorf("system backup operation not found")
	}
	return s, nil
}
func (m *Manager) Restore(ctx context.Context, id string) (Status, error) {
	if id == "" || filepath.Base(id) != id {
		return Status{}, fmt.Errorf("invalid recovery set id")
	}
	path := filepath.Join(m.Root, id)
	if _, err := os.Stat(filepath.Join(path, "manifest.json")); err != nil {
		return Status{}, fmt.Errorf("recovery set is not staged: %w", err)
	}
	now := time.Now().UTC()
	s := Status{ID: id, Status: "running", RecoverySetPath: path, CreatedAt: now, UpdatedAt: now}
	m.put(s)
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := exec.CommandContext(c, m.Zonctl, "restore", "--recovery-set", path, "--confirm", "restore-recovery-set", "--acknowledge-data-loss", "--output", "json").Run()
		m.mu.Lock()
		defer m.mu.Unlock()
		v := m.operations[id]
		v.UpdatedAt = time.Now().UTC()
		if err != nil {
			v.Status = "failed"
			v.Message = "restore failed"
		} else {
			v.Status = "succeeded"
			v.Message = "system data restored; import configuration archive"
		}
		m.operations[id] = v
	}()
	return s, nil
}
func (m *Manager) put(s Status) { m.mu.Lock(); defer m.mu.Unlock(); m.operations[s.ID] = s }
