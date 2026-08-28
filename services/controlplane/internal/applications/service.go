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
	"sort"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

var (
	ErrInvalidDefinition = errors.New("applications: invalid definition")
	ErrNotFound          = errors.New("applications: not found")
	ErrCatalogReadOnly   = errors.New("applications: catalog is release-provided and read-only")
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
		Port      int           `json:"port,omitempty"`
		Endpoints []Endpoint    `json:"endpoints,omitempty"`
		Security  SecurityGrant `json:"security,omitempty"`
	} `json:"runtime"`
}

// Endpoint is a catalog-reviewed exposure grant. A direct endpoint is backed
// by one ServiceLB LoadBalancer Service and a matching host firewall policy.
type Endpoint struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort,omitempty"`
	Direct     bool   `json:"direct,omitempty"`
	MDNS       *MDNS  `json:"mdns,omitempty"`
}

type MDNS struct {
	ServiceType string `json:"serviceType"`
	Instance    string `json:"instance,omitempty"`
}

// SecurityGrant can be present only in the signed appliance catalog. These
// fields are never accepted as raw Kubernetes resources from an administrator.
type SecurityGrant struct {
	HostNetwork bool `json:"hostNetwork,omitempty"`
	Privileged  bool `json:"privileged,omitempty"`
}

// Catalog is injected from the signed release configuration. It is immutable
// at runtime; an update is a normal signed appliance upgrade.
type Catalog struct {
	Applications []Definition `json:"applications"`
}

// ResourceManager applies the stable Kubernetes contract for an application.
// It deliberately has no dependency on Automation Runtime or workflows.
type ResourceManager interface {
	Apply(ctx context.Context, definition Definition) (string, error)
	Delete(ctx context.Context, name string) error
}

type Service struct {
	store   storage.ApplicationStore
	catalog map[string]Definition
}

func NewService(store storage.ApplicationStore, catalogs ...Catalog) (*Service, error) {
	if store == nil {
		return nil, errors.New("applications: store is required")
	}
	catalog := map[string]Definition{}
	for _, supplied := range catalogs {
		for _, definition := range supplied.Applications {
			if err := validate(definition); err != nil {
				return nil, err
			}
			key := catalogKey(definition.Metadata.Name, definition.Metadata.Version)
			if _, exists := catalog[key]; exists {
				return nil, fmt.Errorf("%w: duplicate catalog entry %s", ErrInvalidDefinition, key)
			}
			catalog[key] = definition
		}
	}
	return &Service{store: store, catalog: catalog}, nil
}

// Register is retained for wire compatibility but rejects mutable application
// definitions. Only a signed release can populate the catalog.
func (s *Service) Register(ctx context.Context, document []byte) (Definition, error) {
	_ = ctx
	_ = document
	return Definition{}, ErrCatalogReadOnly
}

func (s *Service) ListDefinitions(ctx context.Context) ([]storage.ApplicationDefinition, error) {
	_ = ctx
	items := make([]storage.ApplicationDefinition, 0, len(s.catalog))
	for _, definition := range s.catalog {
		document, err := json.Marshal(definition)
		if err != nil {
			return nil, fmt.Errorf("applications: encode catalog entry: %w", err)
		}
		items = append(items, storage.ApplicationDefinition{Name: definition.Metadata.Name, Version: definition.Metadata.Version, Document: document})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Version < items[j].Version
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *Service) GetDefinition(ctx context.Context, name, version string) (storage.ApplicationDefinition, error) {
	_ = ctx
	definition, ok := s.catalog[catalogKey(name, version)]
	if !ok {
		return storage.ApplicationDefinition{}, ErrNotFound
	}
	document, err := json.Marshal(definition)
	if err != nil {
		return storage.ApplicationDefinition{}, fmt.Errorf("applications: encode catalog entry: %w", err)
	}
	return storage.ApplicationDefinition{Name: definition.Metadata.Name, Version: definition.Metadata.Version, Document: document}, nil
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
	seenEndpoints := map[string]struct{}{}
	for _, endpoint := range d.Runtime.Endpoints {
		if !namePattern.MatchString(endpoint.Name) || endpoint.Port < 1 || endpoint.Port > 65535 || (endpoint.Protocol != "TCP" && endpoint.Protocol != "UDP") {
			return fmt.Errorf("%w: runtime.endpoints must use a DNS-label name, TCP/UDP, and a valid port", ErrInvalidDefinition)
		}
		if _, exists := seenEndpoints[endpoint.Name]; exists {
			return fmt.Errorf("%w: runtime.endpoints names must be unique", ErrInvalidDefinition)
		}
		seenEndpoints[endpoint.Name] = struct{}{}
		if endpoint.Direct && endpoint.TargetPort == 0 {
			endpoint.TargetPort = endpoint.Port
		}
		if endpoint.MDNS != nil && !endpoint.Direct {
			return fmt.Errorf("%w: mdns requires a direct endpoint", ErrInvalidDefinition)
		}
	}
	if d.Runtime.Security.HostNetwork && len(d.Runtime.Endpoints) == 0 {
		return fmt.Errorf("%w: hostNetwork requires an explicit catalog endpoint", ErrInvalidDefinition)
	}
	return nil
}

func catalogKey(name, version string) string {
	return strings.TrimSpace(name) + "@" + strings.TrimSpace(version)
}
