package configuration

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlstore"
)

func TestImportRequiresMatchingStagedMetadataBundle(t *testing.T) {
	ctx := context.Background()
	db, err := sqlstore.OpenSQLite(filepath.Join(t.TempDir(), "controlplane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	metadata := sqlstore.NewMetadataBundleStore(db)
	service, err := NewService(Stores{
		DB: db, Revisions: sqlstore.NewConfigurationRevisionStore(db), Licensing: sqlstore.NewLicensingStore(db),
		DNS: sqlstore.NewDNSRecordStore(db), BuilderCatalog: sqlstore.NewBuilderCatalogStore(db),
		Applications: sqlstore.NewApplicationStore(db), Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	payload, err := json.Marshal(Snapshot{
		APIVersion: SchemaVersion, CustomProfiles: []CustomProfile{}, DNSRecords: []DNSRecord{},
		BuilderCatalog:  BuilderCatalog{Document: "", ContentType: "application/json"},
		ApplicationDefs: []ApplicationDefinition{}, ApplicationInstances: []ApplicationInstance{},
		Metadata: &MetadataBundleIdentity{Version: "metadata-v1", SoftwareVersion: "release-v1", Digest: "sha256:metadata", Signature: "signature-v1"},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := service.Import(ctx, payload, "operator"); !errors.Is(err, ErrIncompatibleSnapshot) {
		t.Fatalf("Import without staged metadata error = %v, want ErrIncompatibleSnapshot", err)
	}
	if err := metadata.PutMetadataBundle(ctx, storage.MetadataBundleRecord{
		Slot: "active", MetadataVersion: "metadata-v1", SoftwareVersion: "release-v1", Digest: "sha256:metadata", Signature: "signature-v1", InstalledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutMetadataBundle: %v", err)
	}
	if _, err := service.Import(ctx, payload, "operator"); err != nil {
		t.Fatalf("Import with matching staged metadata: %v", err)
	}
	reconciled, err := service.ReconcileLatest(ctx)
	if err != nil {
		t.Fatalf("ReconcileLatest: %v", err)
	}
	if reconciled.Status != storage.ConfigurationRevisionApplied || reconciled.AppliedAt == nil {
		t.Fatalf("reconciliation state = %#v, want applied with timestamp", reconciled)
	}
}

func TestReplacePreservesPriorRevisionAndReplacesDesiredState(t *testing.T) {
	ctx := context.Background()
	db, err := sqlstore.OpenSQLite(filepath.Join(t.TempDir(), "controlplane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	service, err := NewService(Stores{
		DB: db, Revisions: sqlstore.NewConfigurationRevisionStore(db), Licensing: sqlstore.NewLicensingStore(db),
		DNS: sqlstore.NewDNSRecordStore(db), BuilderCatalog: sqlstore.NewBuilderCatalogStore(db),
		Applications: sqlstore.NewApplicationStore(db), Metadata: sqlstore.NewMetadataBundleStore(db),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	oldPayload, err := json.Marshal(Snapshot{
		APIVersion: SchemaVersion, CustomProfiles: []CustomProfile{{ID: "old", Capabilities: json.RawMessage(`[]`)}},
		DNSRecords:     []DNSRecord{{Name: "old", IPv4: "192.0.2.10", TTL: 60, Source: string(storage.DNSRecordSourceAdmin)}},
		BuilderCatalog: BuilderCatalog{Document: "", ContentType: "application/json"}, ApplicationDefs: []ApplicationDefinition{}, ApplicationInstances: []ApplicationInstance{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Import(ctx, oldPayload, "operator"); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	newPayload, err := json.Marshal(Snapshot{
		APIVersion: SchemaVersion, CustomProfiles: []CustomProfile{{ID: "new", Capabilities: json.RawMessage(`[]`)}},
		DNSRecords:     []DNSRecord{{Name: "new", IPv4: "192.0.2.20", TTL: 120, Source: string(storage.DNSRecordSourceAdmin)}},
		BuilderCatalog: BuilderCatalog{Document: "", ContentType: "application/json"}, ApplicationDefs: []ApplicationDefinition{}, ApplicationInstances: []ApplicationInstance{},
	})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := service.Replace(ctx, newPayload, "operator")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if replacement.ApplyMessage != "configuration replaced; reconciliation pending" {
		t.Fatalf("message = %q", replacement.ApplyMessage)
	}
	profiles, err := service.stores.Licensing.ListCustomProfiles(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].ID != "new" {
		t.Fatalf("profiles = %#v, %v", profiles, err)
	}
	records, err := service.stores.DNS.List(ctx)
	if err != nil || len(records) != 1 || records[0].Name != "new" {
		t.Fatalf("DNS records = %#v, %v", records, err)
	}
	latest, err := service.Latest(ctx)
	if err != nil || latest.ID != replacement.ID {
		t.Fatalf("latest = %#v, %v", latest, err)
	}
}
