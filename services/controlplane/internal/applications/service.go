// Package applications contains the modular-monolith Application Management
// capability. It deliberately has no dependency on Automation Runtime or
// developer workflow execution.
package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

var (
	ErrInvalidDefinition = errors.New("applications: invalid definition")
	ErrNotFound          = errors.New("applications: not found")
)

type Definition struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description,omitempty"`
	} `json:"metadata"`
	Runtime struct {
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Port int `json:"port,omitempty"`
	} `json:"runtime"`
}

// ResourceManager applies the stable Kubernetes contract for an application.
// It deliberately has no dependency on Automation Runtime or workflows.
type ResourceManager interface {
	Apply(ctx context.Context, definition Definition) (string, error)
	Delete(ctx context.Context, name string) error
}

type Service struct {
	store storage.ApplicationStore
}

func NewService(store storage.ApplicationStore) (*Service, error) {
	if store == nil {
		return nil, errors.New("applications: store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) Register(ctx context.Context, document []byte) (Definition, error) {
	var definition Definition
	if err := json.Unmarshal(document, &definition); err != nil {
		return Definition{}, fmt.Errorf("%w: JSON: %v", ErrInvalidDefinition, err)
	}
	if err := validate(definition); err != nil {
		return Definition{}, err
	}
	now := time.Now().UTC()
	if err := s.store.UpsertDefinition(ctx, storage.ApplicationDefinition{
		Name: definition.Metadata.Name, Version: definition.Metadata.Version, Document: append([]byte(nil), document...), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func (s *Service) ListDefinitions(ctx context.Context) ([]storage.ApplicationDefinition, error) {
	return s.store.ListDefinitions(ctx)
}

func (s *Service) GetDefinition(ctx context.Context, name, version string) (storage.ApplicationDefinition, error) {
	d, err := s.store.GetDefinition(ctx, name, version)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.ApplicationDefinition{}, ErrNotFound
	}
	return d, err
}

func (s *Service) Install(ctx context.Context, name, version string) (storage.ApplicationInstance, error) {
	d, err := s.GetDefinition(ctx, name, version)
	if err != nil {
		return storage.ApplicationInstance{}, err
	}
	now := time.Now().UTC()
	i := storage.ApplicationInstance{
		Name: name, DefinitionName: d.Name, DefinitionVersion: d.Version,
		DesiredState: "running", ObservedState: "pending",
		Message: "application accepted for reconciliation", CreatedAt: now, UpdatedAt: now,
	}
	if existing, getErr := s.store.GetInstance(ctx, name); getErr == nil {
		i.CreatedAt = existing.CreatedAt
	}
	if err := s.store.UpsertInstance(ctx, i); err != nil {
		return storage.ApplicationInstance{}, err
	}
	return i, nil
}

func (s *Service) ListInstances(ctx context.Context) ([]storage.ApplicationInstance, error) {
	return s.store.ListInstances(ctx)
}

func (s *Service) GetInstance(ctx context.Context, name string) (storage.ApplicationInstance, error) {
	i, err := s.store.GetInstance(ctx, name)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.ApplicationInstance{}, ErrNotFound
	}
	return i, err
}

// ReconcileAll converges accepted application instances independently. A
// transient Kubernetes failure is recorded and retried on the next pass.
func (s *Service) ReconcileAll(ctx context.Context, manager ResourceManager) error {
	if manager == nil {
		return nil
	}
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if instance.DesiredState != "running" {
			continue
		}
		definition, err := s.GetDefinition(ctx, instance.DefinitionName, instance.DefinitionVersion)
		if err != nil {
			return err
		}
		var parsed Definition
		if err := json.Unmarshal(definition.Document, &parsed); err != nil {
			return fmt.Errorf("applications: decode %s: %w", instance.Name, err)
		}
		observed, err := manager.Apply(ctx, parsed)
		message := "application reconciled"
		if err != nil {
			observed = "error"
			message = err.Error()
		}
		if statusErr := s.store.UpdateInstanceStatus(ctx, instance.Name, observed, message, time.Now().UTC()); statusErr != nil {
			return statusErr
		}
	}
	return nil
}

func validate(d Definition) error {
	if d.APIVersion != "appliance.zon/v1" || d.Kind != "ApplicationDefinition" {
		return fmt.Errorf("%w: apiVersion must be appliance.zon/v1 and kind must be ApplicationDefinition", ErrInvalidDefinition)
	}
	if !namePattern.MatchString(d.Metadata.Name) || len(d.Metadata.Name) > 63 {
		return fmt.Errorf("%w: metadata.name must be a DNS label", ErrInvalidDefinition)
	}
	if strings.TrimSpace(d.Metadata.Version) == "" {
		return fmt.Errorf("%w: metadata.version is required", ErrInvalidDefinition)
	}
	image := strings.TrimSpace(d.Runtime.Image.Reference)
	if !strings.HasPrefix(image, "registry.local/") || !strings.Contains(image, "@sha256:") {
		return fmt.Errorf("%w: runtime.image.reference must be a local digest-pinned image", ErrInvalidDefinition)
	}
	return nil
}
