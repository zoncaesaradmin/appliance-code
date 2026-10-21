package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

func TestConfigurationRevisionStorePreservesImmutablePayloadAndApplyStatus(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "configuration.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := NewConfigurationRevisionStore(db)
	createdAt := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	payload := []byte(`{"apiVersion":"configuration.zon/v1","dnsRecords":[]}`)
	if err := store.CreateConfigurationRevision(ctx, storage.ConfigurationRevision{
		ID: "config-1", SchemaVersion: "configuration.zon/v1", Payload: payload,
		PayloadDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreatedAt:     createdAt, CreatedBy: "admin-1",
	}); err != nil {
		t.Fatalf("CreateConfigurationRevision: %v", err)
	}

	appliedAt := createdAt.Add(time.Minute)
	if err := store.SetConfigurationRevisionStatus(ctx, "config-1", storage.ConfigurationRevisionApplied, "reconciled", &appliedAt); err != nil {
		t.Fatalf("SetConfigurationRevisionStatus: %v", err)
	}
	got, err := store.LatestConfigurationRevision(ctx)
	if err != nil {
		t.Fatalf("LatestConfigurationRevision: %v", err)
	}
	if string(got.Payload) != string(payload) || got.PayloadDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("stored revision payload = %#v", got)
	}
	if got.Status != storage.ConfigurationRevisionApplied || got.AppliedAt == nil || !got.AppliedAt.Equal(appliedAt) {
		t.Fatalf("stored revision apply status = %#v", got)
	}
	if err := store.SetConfigurationRevisionStatus(ctx, "missing", storage.ConfigurationRevisionFailed, "missing", nil); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("SetConfigurationRevisionStatus missing error = %v, want ErrNotFound", err)
	}
}
