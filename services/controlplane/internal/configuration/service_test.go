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
}
