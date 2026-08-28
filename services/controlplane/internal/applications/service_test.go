package applications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type memoryStore struct {
	definitions []storage.ApplicationDefinition
	instances   []storage.ApplicationInstance
}

func (m *memoryStore) UpsertDefinition(_ context.Context, d storage.ApplicationDefinition) error {
	m.definitions = append(m.definitions, d)
	return nil
}
func (m *memoryStore) GetDefinition(_ context.Context, name, version string) (storage.ApplicationDefinition, error) {
	for _, d := range m.definitions {
		if d.Name == name && d.Version == version {
			return d, nil
		}
	}
	return storage.ApplicationDefinition{}, storage.ErrNotFound
}
func (m *memoryStore) ListDefinitions(context.Context) ([]storage.ApplicationDefinition, error) {
	return m.definitions, nil
}
func (m *memoryStore) UpsertInstance(_ context.Context, i storage.ApplicationInstance) error {
	for index := range m.instances {
		if m.instances[index].Name == i.Name {
			m.instances[index] = i
			return nil
		}
	}
	m.instances = append(m.instances, i)
	return nil
}
func (m *memoryStore) UpdateInstanceStatus(_ context.Context, name, observedState, message string, _ time.Time) error {
	for i := range m.instances {
		if m.instances[i].Name == name {
			m.instances[i].ObservedState = observedState
			m.instances[i].Message = message
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *memoryStore) GetInstance(_ context.Context, name string) (storage.ApplicationInstance, error) {
	for _, i := range m.instances {
		if i.Name == name {
			return i, nil
		}
	}
	return storage.ApplicationInstance{}, storage.ErrNotFound
}
func (m *memoryStore) ListInstances(context.Context) ([]storage.ApplicationInstance, error) {
	return m.instances, nil
}

func TestCatalogInstall(t *testing.T) {
	var definition Definition
	if err := json.Unmarshal([]byte(`{"apiVersion":"appliance.zon/v1","kind":"ApplicationDefinition","metadata":{"name":"camera","version":"1.0.0"},"runtime":{"image":{"reference":"registry.local/camera@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`), &definition); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(&memoryStore{}, Catalog{Applications: []Definition{definition}})
	if err != nil {
		t.Fatal(err)
	}
	i, err := svc.Install(context.Background(), "camera", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if i.DesiredState != "running" || i.ObservedState != "pending" {
		t.Fatalf("unexpected instance: %+v", i)
	}
}

func TestCatalogRejectsUnpinnedImageAndRuntimeRegistration(t *testing.T) {
	var invalid Definition
	if err := json.Unmarshal([]byte(`{"apiVersion":"appliance.zon/v1","kind":"ApplicationDefinition","metadata":{"name":"camera","version":"1.0.0"},"runtime":{"image":{"reference":"docker.io/camera:latest"}}}`), &invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(&memoryStore{}, Catalog{Applications: []Definition{invalid}}); err == nil {
		t.Fatal("expected invalid catalog image rejection")
	}
	svc, _ := NewService(&memoryStore{})
	if _, err := svc.Register(context.Background(), []byte(`{}`)); !errors.Is(err, ErrCatalogReadOnly) {
		t.Fatalf("Register error = %v, want ErrCatalogReadOnly", err)
	}
}

type recordingManager struct {
	applied   []string
	deleted   []string
	applyErr  error
	deleteErr error
}

func (m *recordingManager) Apply(_ context.Context, definition Definition) (string, error) {
	m.applied = append(m.applied, definition.Metadata.Name)
	return "running", m.applyErr
}

func (m *recordingManager) Delete(_ context.Context, definition Definition) error {
	m.deleted = append(m.deleted, definition.Metadata.Name)
	return m.deleteErr
}

func TestDisableWithdrawsResourcesOnReconcile(t *testing.T) {
	var definition Definition
	if err := json.Unmarshal([]byte(`{"apiVersion":"appliance.zon/v1","kind":"ApplicationDefinition","metadata":{"name":"jellyfin","version":"1.0.0"},"runtime":{"image":{"reference":"registry.local/jellyfin@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"endpoints":[{"name":"http","protocol":"TCP","port":8096,"direct":true}]}}`), &definition); err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{}
	svc, err := NewService(store, Catalog{Applications: []Definition{definition}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Install(context.Background(), "jellyfin", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Disable(context.Background(), "jellyfin"); err != nil {
		t.Fatal(err)
	}
	manager := &recordingManager{}
	if err := svc.ReconcileAll(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	if len(manager.applied) != 0 || len(manager.deleted) != 1 || manager.deleted[0] != "jellyfin" {
		t.Fatalf("manager calls = applied %v deleted %v", manager.applied, manager.deleted)
	}
	instance, err := svc.GetInstance(context.Background(), "jellyfin")
	if err != nil {
		t.Fatal(err)
	}
	if instance.ObservedState != "stopped" || instance.Message != "application withdrawn" {
		t.Fatalf("instance = %+v", instance)
	}
}
