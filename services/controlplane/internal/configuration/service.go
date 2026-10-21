// Package configuration owns the replayable desired-configuration boundary.
// It intentionally does not model credentials, sessions, audit history,
// transient jobs, or raw database backups; those are separate recovery-set
// concerns with stronger authorization and encryption requirements.
package configuration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/storage"
)

const SchemaVersion = "configuration.zon/v1"

// ErrInvalidSnapshot means a payload does not satisfy the portable
// desired-configuration schema. Callers can report it as an invalid archive
// or operator input rather than an internal service failure.
var ErrInvalidSnapshot = errors.New("configuration: invalid snapshot")

// ErrDestinationNotClean means an import would merge into pre-existing
// desired state. Configuration restore is intentionally replace-only and
// must use an explicitly clean target.
var ErrDestinationNotClean = errors.New("configuration: destination is not clean")

// Snapshot is the portable, desired-state-only configuration payload. The
// database records only metadata-bundle identity: payload bytes for a signed
// bundle are validated and staged by the installer/recovery coordinator.
type Snapshot struct {
	APIVersion           string                  `json:"apiVersion"`
	CustomProfiles       []CustomProfile         `json:"customProfiles"`
	ProfileActivation    ProfileActivation       `json:"profileActivation"`
	DNSRecords           []DNSRecord             `json:"dnsRecords"`
	BuilderCatalog       BuilderCatalog          `json:"builderCatalog"`
	ApplicationDefs      []ApplicationDefinition `json:"applicationDefinitions"`
	ApplicationInstances []ApplicationInstance   `json:"applicationInstances"`
	Metadata             *MetadataBundleIdentity `json:"metadataBundle,omitempty"`
}

type CustomProfile struct {
	ID           string          `json:"id"`
	DisplayName  string          `json:"displayName"`
	Description  string          `json:"description,omitempty"`
	Capabilities json.RawMessage `json:"capabilities"`
}

type ProfileActivation struct {
	DesiredProfileID string `json:"desiredProfileId,omitempty"`
}

type DNSRecord struct {
	Name   string `json:"name"`
	IPv4   string `json:"ipv4"`
	TTL    int    `json:"ttl"`
	Source string `json:"source"`
	Owner  string `json:"owner,omitempty"`
}

type BuilderCatalog struct {
	Document    string `json:"document"`
	ContentType string `json:"contentType"`
}

type ApplicationDefinition struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Document json.RawMessage `json:"document"`
}

type ApplicationInstance struct {
	Name              string `json:"name"`
	DefinitionName    string `json:"definitionName"`
	DefinitionVersion string `json:"definitionVersion"`
	DesiredState      string `json:"desiredState"`
}

type MetadataBundleIdentity struct {
	Version         string `json:"version"`
	SoftwareVersion string `json:"softwareVersion"`
	Digest          string `json:"digest"`
	Signature       string `json:"signature,omitempty"`
}

// Stores groups the existing domain repositories used to form a coherent
// desired-state snapshot. They must all be constructed from the same DB.
type Stores struct {
	DB             storage.DB
	Revisions      storage.ConfigurationRevisionStore
	Licensing      storage.LicensingStore
	DNS            storage.DNSRecordStore
	BuilderCatalog storage.BuilderCatalogStore
	Applications   storage.ApplicationStore
	Metadata       storage.MetadataBundleStore
}

type Service struct{ stores Stores }

func NewService(stores Stores) (*Service, error) {
	if stores.DB == nil || stores.Revisions == nil || stores.Licensing == nil || stores.DNS == nil || stores.BuilderCatalog == nil || stores.Applications == nil || stores.Metadata == nil {
		return nil, fmt.Errorf("configuration: all persistence stores are required")
	}
	return &Service{stores: stores}, nil
}

// Capture creates a new immutable revision from the control plane's current
// desired configuration. Capture runs all reads and revision creation inside
// one database transaction, avoiding a mixed-time export.
func (s *Service) Capture(ctx context.Context, actorID string) (storage.ConfigurationRevision, error) {
	var created storage.ConfigurationRevision
	err := s.stores.DB.WithTx(ctx, func(txCtx context.Context) error {
		snapshot, err := s.snapshot(txCtx)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("configuration: encoding snapshot: %w", err)
		}
		sum := sha256.Sum256(payload)
		created = storage.ConfigurationRevision{
			ID:            uuid.NewString(),
			SchemaVersion: SchemaVersion,
			Payload:       payload,
			PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]),
			CreatedAt:     time.Now().UTC(),
			CreatedBy:     strings.TrimSpace(actorID),
			Status:        storage.ConfigurationRevisionPending,
		}
		return s.stores.Revisions.CreateConfigurationRevision(txCtx, created)
	})
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, id string) (storage.ConfigurationRevision, error) {
	return s.stores.Revisions.GetConfigurationRevision(ctx, id)
}

func (s *Service) Latest(ctx context.Context) (storage.ConfigurationRevision, error) {
	return s.stores.Revisions.LatestConfigurationRevision(ctx)
}

// Import validates and persists a desired-state snapshot into an empty
// configuration destination. It deliberately refuses a merge: configuration
// restore is a clean-target operation, and applying a backup over unknown
// desired state would make the result impossible to reason about or roll back.
// Metadata bundle files themselves are staged and signature-validated by the
// installer before this method is called.
func (s *Service) Import(ctx context.Context, payload []byte, actorID string) (storage.ConfigurationRevision, error) {
	snapshot, canonicalPayload, err := validateSnapshot(payload)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	var imported storage.ConfigurationRevision
	err = s.stores.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.ensureEmpty(txCtx); err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, profile := range snapshot.CustomProfiles {
			if err := s.stores.Licensing.UpsertCustomProfile(txCtx, storage.CustomApplianceProfile{
				ID: profile.ID, DisplayName: profile.DisplayName, Description: profile.Description,
				CapabilitiesJSON: string(profile.Capabilities), CreatedAt: now, UpdatedAt: now,
				CreatedByUserID: strings.TrimSpace(actorID),
			}); err != nil {
				return fmt.Errorf("configuration: import custom profile %q: %w", profile.ID, err)
			}
		}
		if err := s.stores.Licensing.PutActivation(txCtx, storage.ProfileActivationRecord{
			DesiredProfileID: snapshot.ProfileActivation.DesiredProfileID,
			Status:           storage.ProfileActivationPendingRestart,
			Message:          "configuration import pending reconciliation",
			UpdatedAt:        now,
			UpdatedByUserID:  strings.TrimSpace(actorID),
		}); err != nil {
			return fmt.Errorf("configuration: import profile activation: %w", err)
		}
		for _, record := range snapshot.DNSRecords {
			if err := s.stores.DNS.Upsert(txCtx, storage.DNSRecord{
				Name: record.Name, IPv4: record.IPv4, TTL: record.TTL,
				Source: storage.DNSRecordSource(record.Source), Owner: record.Owner,
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return fmt.Errorf("configuration: import DNS record %q: %w", record.Name, err)
			}
		}
		if err := s.stores.BuilderCatalog.PutBuilderCatalog(txCtx, storage.BuilderCatalogRecord{
			DocumentText: snapshot.BuilderCatalog.Document, ContentType: snapshot.BuilderCatalog.ContentType, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("configuration: import builder catalog: %w", err)
		}
		for _, definition := range snapshot.ApplicationDefs {
			if err := s.stores.Applications.UpsertDefinition(txCtx, storage.ApplicationDefinition{
				Name: definition.Name, Version: definition.Version, Document: definition.Document, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return fmt.Errorf("configuration: import application definition %s@%s: %w", definition.Name, definition.Version, err)
			}
		}
		for _, instance := range snapshot.ApplicationInstances {
			if err := s.stores.Applications.UpsertInstance(txCtx, storage.ApplicationInstance{
				Name: instance.Name, DefinitionName: instance.DefinitionName, DefinitionVersion: instance.DefinitionVersion,
				DesiredState: instance.DesiredState, ObservedState: "pending", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return fmt.Errorf("configuration: import application instance %q: %w", instance.Name, err)
			}
		}
		sum := sha256.Sum256(canonicalPayload)
		imported = storage.ConfigurationRevision{
			ID:            uuid.NewString(),
			SchemaVersion: SchemaVersion,
			Payload:       canonicalPayload,
			PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]),
			CreatedAt:     now,
			CreatedBy:     strings.TrimSpace(actorID),
			Status:        storage.ConfigurationRevisionPending,
			ApplyMessage:  "imported; reconciliation pending",
		}
		return s.stores.Revisions.CreateConfigurationRevision(txCtx, imported)
	})
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	return imported, nil
}

func (s *Service) ensureEmpty(ctx context.Context) error {
	if _, err := s.stores.Revisions.LatestConfigurationRevision(ctx); err == nil {
		return fmt.Errorf("%w: configuration revision already exists", ErrDestinationNotClean)
	} else if err != storage.ErrNotFound {
		return fmt.Errorf("configuration: inspect destination revisions: %w", err)
	}
	profiles, err := s.stores.Licensing.ListCustomProfiles(ctx)
	if err != nil {
		return fmt.Errorf("configuration: inspect destination profiles: %w", err)
	}
	dnsRecords, err := s.stores.DNS.List(ctx)
	if err != nil {
		return fmt.Errorf("configuration: inspect destination DNS records: %w", err)
	}
	definitions, err := s.stores.Applications.ListDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("configuration: inspect destination application definitions: %w", err)
	}
	instances, err := s.stores.Applications.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("configuration: inspect destination application instances: %w", err)
	}
	if len(profiles) != 0 || len(dnsRecords) != 0 || len(definitions) != 0 || len(instances) != 0 {
		return fmt.Errorf("%w: desired configuration already exists", ErrDestinationNotClean)
	}
	return nil
}

func validateSnapshot(payload []byte) (Snapshot, []byte, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: decode snapshot: %v", ErrInvalidSnapshot, err)
	}
	if snapshot.APIVersion != SchemaVersion {
		return Snapshot{}, nil, fmt.Errorf("%w: unsupported snapshot apiVersion %q", ErrInvalidSnapshot, snapshot.APIVersion)
	}
	seenProfiles, seenDNS, seenDefinitions, seenInstances := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, profile := range snapshot.CustomProfiles {
		if profile.ID == "" || !json.Valid(profile.Capabilities) {
			return Snapshot{}, nil, fmt.Errorf("%w: invalid custom profile", ErrInvalidSnapshot)
		}
		if _, exists := seenProfiles[profile.ID]; exists {
			return Snapshot{}, nil, fmt.Errorf("%w: duplicate custom profile %q", ErrInvalidSnapshot, profile.ID)
		}
		seenProfiles[profile.ID] = struct{}{}
	}
	for _, record := range snapshot.DNSRecords {
		if record.Name == "" || record.IPv4 == "" || record.TTL <= 0 {
			return Snapshot{}, nil, fmt.Errorf("%w: invalid DNS record", ErrInvalidSnapshot)
		}
		if _, exists := seenDNS[record.Name]; exists {
			return Snapshot{}, nil, fmt.Errorf("%w: duplicate DNS record %q", ErrInvalidSnapshot, record.Name)
		}
		seenDNS[record.Name] = struct{}{}
	}
	for _, definition := range snapshot.ApplicationDefs {
		key := definition.Name + "@" + definition.Version
		if definition.Name == "" || definition.Version == "" || !json.Valid(definition.Document) {
			return Snapshot{}, nil, fmt.Errorf("%w: invalid application definition", ErrInvalidSnapshot)
		}
		if _, exists := seenDefinitions[key]; exists {
			return Snapshot{}, nil, fmt.Errorf("%w: duplicate application definition %q", ErrInvalidSnapshot, key)
		}
		seenDefinitions[key] = struct{}{}
	}
	for _, instance := range snapshot.ApplicationInstances {
		if instance.Name == "" || instance.DefinitionName == "" || instance.DefinitionVersion == "" || instance.DesiredState == "" {
			return Snapshot{}, nil, fmt.Errorf("%w: invalid application instance", ErrInvalidSnapshot)
		}
		if _, exists := seenInstances[instance.Name]; exists {
			return Snapshot{}, nil, fmt.Errorf("%w: duplicate application instance %q", ErrInvalidSnapshot, instance.Name)
		}
		seenInstances[instance.Name] = struct{}{}
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("configuration: canonicalize snapshot: %w", err)
	}
	return snapshot, canonical, nil
}

func (s *Service) snapshot(ctx context.Context) (Snapshot, error) {
	profiles, err := s.stores.Licensing.ListCustomProfiles(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: list custom profiles: %w", err)
	}
	activation, err := s.stores.Licensing.GetActivation(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: get profile activation: %w", err)
	}
	dnsRecords, err := s.stores.DNS.List(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: list DNS records: %w", err)
	}
	catalog, err := s.stores.BuilderCatalog.GetBuilderCatalog(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: get builder catalog: %w", err)
	}
	definitions, err := s.stores.Applications.ListDefinitions(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: list application definitions: %w", err)
	}
	instances, err := s.stores.Applications.ListInstances(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("configuration: list application instances: %w", err)
	}

	snapshot := Snapshot{
		APIVersion:        SchemaVersion,
		ProfileActivation: ProfileActivation{DesiredProfileID: activation.DesiredProfileID},
		BuilderCatalog:    BuilderCatalog{Document: catalog.DocumentText, ContentType: catalog.ContentType},
	}
	for _, profile := range profiles {
		capabilities := json.RawMessage(profile.CapabilitiesJSON)
		if !json.Valid(capabilities) {
			return Snapshot{}, fmt.Errorf("configuration: custom profile %q has invalid persisted capabilities JSON", profile.ID)
		}
		snapshot.CustomProfiles = append(snapshot.CustomProfiles, CustomProfile{ID: profile.ID, DisplayName: profile.DisplayName, Description: profile.Description, Capabilities: capabilities})
	}
	for _, record := range dnsRecords {
		snapshot.DNSRecords = append(snapshot.DNSRecords, DNSRecord{Name: record.Name, IPv4: record.IPv4, TTL: record.TTL, Source: string(record.Source), Owner: record.Owner})
	}
	for _, definition := range definitions {
		if !json.Valid(definition.Document) {
			return Snapshot{}, fmt.Errorf("configuration: application definition %s@%s is not JSON", definition.Name, definition.Version)
		}
		snapshot.ApplicationDefs = append(snapshot.ApplicationDefs, ApplicationDefinition{Name: definition.Name, Version: definition.Version, Document: definition.Document})
	}
	for _, instance := range instances {
		snapshot.ApplicationInstances = append(snapshot.ApplicationInstances, ApplicationInstance{Name: instance.Name, DefinitionName: instance.DefinitionName, DefinitionVersion: instance.DefinitionVersion, DesiredState: instance.DesiredState})
	}
	if active, err := s.stores.Metadata.GetMetadataBundle(ctx, "active"); err == nil {
		snapshot.Metadata = &MetadataBundleIdentity{Version: active.MetadataVersion, SoftwareVersion: active.SoftwareVersion, Digest: active.Digest, Signature: active.Signature}
	} else if err != storage.ErrNotFound {
		return Snapshot{}, fmt.Errorf("configuration: get active metadata bundle: %w", err)
	}

	// Repository order is normally deterministic. Sort defensively so the
	// archived bytes and digest do not vary by storage engine/query plan.
	sort.Slice(snapshot.CustomProfiles, func(i, j int) bool { return snapshot.CustomProfiles[i].ID < snapshot.CustomProfiles[j].ID })
	sort.Slice(snapshot.DNSRecords, func(i, j int) bool { return snapshot.DNSRecords[i].Name < snapshot.DNSRecords[j].Name })
	sort.Slice(snapshot.ApplicationDefs, func(i, j int) bool {
		if snapshot.ApplicationDefs[i].Name == snapshot.ApplicationDefs[j].Name {
			return snapshot.ApplicationDefs[i].Version < snapshot.ApplicationDefs[j].Version
		}
		return snapshot.ApplicationDefs[i].Name < snapshot.ApplicationDefs[j].Name
	})
	sort.Slice(snapshot.ApplicationInstances, func(i, j int) bool {
		return snapshot.ApplicationInstances[i].Name < snapshot.ApplicationInstances[j].Name
	})
	return snapshot, nil
}
