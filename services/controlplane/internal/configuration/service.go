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

// ErrIncompatibleSnapshot means a valid archive cannot be applied to this
// target because a declared prerequisite (for example a signed metadata
// bundle) is absent or differs. It is distinct from malformed input and must
// be surfaced before any target configuration is changed.
var ErrIncompatibleSnapshot = errors.New("configuration: incompatible snapshot")

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

// ReconcileLatest revalidates the latest persisted desired-state snapshot at
// startup and records a durable state transition. Import writes all supported
// desired-state tables atomically; this step proves the resulting snapshot is
// still compatible with the staged metadata before readiness is advertised.
// A failed attempt is retried on the next process start rather than being
// silently represented as applied.
func (s *Service) ReconcileLatest(ctx context.Context) (storage.ConfigurationRevision, error) {
	revision, err := s.stores.Revisions.LatestConfigurationRevision(ctx)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	if revision.Status == storage.ConfigurationRevisionApplied {
		return revision, nil
	}
	if err := s.stores.Revisions.SetConfigurationRevisionStatus(ctx, revision.ID, storage.ConfigurationRevisionApplying, "validating persisted desired configuration", nil); err != nil {
		return storage.ConfigurationRevision{}, err
	}
	_, _, reconcileErr := validateSnapshot(revision.Payload)
	if reconcileErr == nil {
		var snapshot Snapshot
		reconcileErr = json.Unmarshal(revision.Payload, &snapshot)
		if reconcileErr == nil {
			reconcileErr = s.validateStagedMetadata(ctx, snapshot.Metadata)
		}
	}
	if reconcileErr != nil {
		_ = s.stores.Revisions.SetConfigurationRevisionStatus(ctx, revision.ID, storage.ConfigurationRevisionFailed, "reconciliation failed: "+reconcileErr.Error(), nil)
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: reconcile revision %s: %w", revision.ID, reconcileErr)
	}
	now := time.Now().UTC()
	if err := s.stores.Revisions.SetConfigurationRevisionStatus(ctx, revision.ID, storage.ConfigurationRevisionApplied, "persisted desired configuration reconciled", &now); err != nil {
		return storage.ConfigurationRevision{}, err
	}
	revision.Status, revision.ApplyMessage, revision.AppliedAt = storage.ConfigurationRevisionApplied, "persisted desired configuration reconciled", &now
	return revision, nil
}

// Import validates and persists a desired-state snapshot into an empty
// configuration destination. Call Replace to explicitly replace the active
// desired configuration of an already-running appliance.
// Metadata bundle files themselves are staged and signature-validated by the
// installer before either method is called.
func (s *Service) Import(ctx context.Context, payload []byte, actorID string) (storage.ConfigurationRevision, error) {
	return s.importSnapshot(ctx, payload, actorID, false)
}

// Replace validates a portable desired-state snapshot, preserves the current
// desired state as an immutable revision, then replaces configuration-owned
// records atomically. It never touches identities, credentials, sessions,
// audit history, blob objects, or other runtime data. Callers must require an
// explicit operator confirmation before using it.
func (s *Service) Replace(ctx context.Context, payload []byte, actorID string) (storage.ConfigurationRevision, error) {
	if _, _, err := validateSnapshot(payload); err != nil {
		return storage.ConfigurationRevision{}, err
	}
	if _, err := s.Capture(ctx, actorID); err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: capture pre-replacement revision: %w", err)
	}
	return s.importSnapshot(ctx, payload, actorID, true)
}

// ReplaceAndReconcile is the runtime configuration-import operation. Unlike
// installer bootstrap, a running appliance has no subsequent process start
// to reconcile a pending revision, so success is not reported until the
// replacement has passed the same durable reconciliation boundary.
func (s *Service) ReplaceAndReconcile(ctx context.Context, payload []byte, actorID string) (storage.ConfigurationRevision, error) {
	imported, err := s.Replace(ctx, payload, actorID)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	reconciled, err := s.ReconcileLatest(ctx)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	if reconciled.ID != imported.ID {
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: replacement reconciliation selected unexpected revision %s", reconciled.ID)
	}
	return reconciled, nil
}

func (s *Service) importSnapshot(ctx context.Context, payload []byte, actorID string, replace bool) (storage.ConfigurationRevision, error) {
	snapshot, canonicalPayload, err := validateSnapshot(payload)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	var imported storage.ConfigurationRevision
	err = s.stores.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.validateStagedMetadata(txCtx, snapshot.Metadata); err != nil {
			return err
		}
		if replace {
			if err := s.clearDesiredState(txCtx); err != nil {
				return err
			}
		} else if err := s.ensureEmpty(txCtx); err != nil {
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
			ApplyMessage:  importMessage(replace),
		}
		return s.stores.Revisions.CreateConfigurationRevision(txCtx, imported)
	})
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	return imported, nil
}

func importMessage(replace bool) string {
	if replace {
		return "configuration replaced; reconciliation pending"
	}
	return "imported; reconciliation pending"
}

// clearDesiredState removes only records represented by Snapshot. It runs in
// the caller transaction and deliberately leaves immutable revisions in place
// so a prior configuration remains auditable and exportable.
func (s *Service) clearDesiredState(ctx context.Context) error {
	instances, err := s.stores.Applications.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("configuration: list instances for replacement: %w", err)
	}
	for _, instance := range instances {
		if err := s.stores.Applications.DeleteInstance(ctx, instance.Name); err != nil {
			return fmt.Errorf("configuration: remove application instance %q: %w", instance.Name, err)
		}
	}
	definitions, err := s.stores.Applications.ListDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("configuration: list definitions for replacement: %w", err)
	}
	for _, definition := range definitions {
		if err := s.stores.Applications.DeleteDefinition(ctx, definition.Name, definition.Version); err != nil {
			return fmt.Errorf("configuration: remove application definition %s@%s: %w", definition.Name, definition.Version, err)
		}
	}
	dnsRecords, err := s.stores.DNS.List(ctx)
	if err != nil {
		return fmt.Errorf("configuration: list DNS records for replacement: %w", err)
	}
	for _, record := range dnsRecords {
		if err := s.stores.DNS.Delete(ctx, record.Name); err != nil {
			return fmt.Errorf("configuration: remove DNS record %q: %w", record.Name, err)
		}
	}
	profiles, err := s.stores.Licensing.ListCustomProfiles(ctx)
	if err != nil {
		return fmt.Errorf("configuration: list custom profiles for replacement: %w", err)
	}
	for _, profile := range profiles {
		if err := s.stores.Licensing.DeleteCustomProfile(ctx, profile.ID); err != nil {
			return fmt.Errorf("configuration: remove custom profile %q: %w", profile.ID, err)
		}
	}
	return nil
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

// validateStagedMetadata ensures an imported desired-state payload cannot
// point to metadata bytes that are absent or different on the target. The
// installer/recovery coordinator stages and verifies signed metadata before
// calling Import; this service only confirms that prerequisite atomically
// with the configuration write.
func (s *Service) validateStagedMetadata(ctx context.Context, expected *MetadataBundleIdentity) error {
	if expected == nil {
		return nil
	}
	active, err := s.stores.Metadata.GetMetadataBundle(ctx, "active")
	if err == storage.ErrNotFound {
		return fmt.Errorf("%w: required active metadata bundle is not staged", ErrIncompatibleSnapshot)
	}
	if err != nil {
		return fmt.Errorf("configuration: inspect active metadata bundle: %w", err)
	}
	if active.MetadataVersion != expected.Version || active.SoftwareVersion != expected.SoftwareVersion || active.Digest != expected.Digest ||
		(expected.Signature != "" && active.Signature != expected.Signature) {
		return fmt.Errorf("%w: staged active metadata bundle does not match archive identity", ErrIncompatibleSnapshot)
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
	if metadata := snapshot.Metadata; metadata != nil && (metadata.Version == "" || metadata.SoftwareVersion == "" || metadata.Digest == "") {
		return Snapshot{}, nil, fmt.Errorf("%w: incomplete metadata bundle identity", ErrInvalidSnapshot)
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
		APIVersion:           SchemaVersion,
		ProfileActivation:    ProfileActivation{DesiredProfileID: activation.DesiredProfileID},
		BuilderCatalog:       BuilderCatalog{Document: catalog.DocumentText, ContentType: catalog.ContentType},
		CustomProfiles:       []CustomProfile{},
		DNSRecords:           []DNSRecord{},
		ApplicationDefs:      []ApplicationDefinition{},
		ApplicationInstances: []ApplicationInstance{},
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
		// Development/bootstrap metadata can exist before it has a signed,
		// immutable identity. It is not portable configuration until all three
		// identity fields are known; omit it rather than emitting an archive
		// that cannot be validated on a target.
		if active.MetadataVersion != "" && active.SoftwareVersion != "" && active.Digest != "" {
			snapshot.Metadata = &MetadataBundleIdentity{Version: active.MetadataVersion, SoftwareVersion: active.SoftwareVersion, Digest: active.Digest, Signature: active.Signature}
		}
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
