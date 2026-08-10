package applications

import (
	"context"
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

func TestRegisterAndInstall(t *testing.T) {
	svc, err := NewService(&memoryStore{})
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{"apiVersion":"appliance.zon/v1","kind":"ApplicationDefinition","metadata":{"name":"camera","version":"1.0.0"},"runtime":{"image":{"reference":"registry.local/camera@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`)
	if _, err := svc.Register(context.Background(), doc); err != nil {
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

func TestRejectsUnpinnedOrRemoteImage(t *testing.T) {
	svc, _ := NewService(&memoryStore{})
	doc := []byte(`{"apiVersion":"appliance.zon/v1","kind":"ApplicationDefinition","metadata":{"name":"camera","version":"1.0.0"},"runtime":{"image":{"reference":"docker.io/camera:latest"}}}`)
	if _, err := svc.Register(context.Background(), doc); err == nil {
		t.Fatal("expected invalid image rejection")
	}
}
