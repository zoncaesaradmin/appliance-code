package profiles_test

import (
	"context"
	"testing"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/profiles"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func openProfiles(t *testing.T, bundle profiles.BundleChecker) (*profiles.Service, *licensing.Service) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := sqlite.NewLicensingStore(db)
	recorder := audit.NewRecorder(sqlite.NewAuditStore(db))
	lic := licensing.NewService(db, store, recorder)
	policy, err := metadatabundle.NewService(db, sqlite.NewMetadataBundleStore(db), recorder, nil, dir)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if bundle == nil {
		bundle = profiles.CompleteBundleChecker{}
	}
	svc := profiles.NewService(db, store, lic, policy, recorder, "core", bundle)
	return svc, lic
}

func TestListProfilesFromMetadata(t *testing.T) {
	svc, _ := openProfiles(t, nil)
	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("expected built-in profiles from metadata bundle")
	}
	found := false
	for _, p := range items {
		if p.ID == "core" && p.BuiltIn {
			found = true
		}
	}
	if !found {
		t.Fatalf("core profile missing: %+v", items)
	}
}

func TestActivateRequiresLicensing(t *testing.T) {
	svc, _ := openProfiles(t, nil)
	_, _, err := svc.Activate(context.Background(), audit.SystemActor, "core")
	if err == nil {
		t.Fatal("expected licensing unresolved")
	}
}

func TestActivateBuiltInCore(t *testing.T) {
	svc, lic := openProfiles(t, nil)
	if _, err := lic.AcceptBaseEntitlement(context.Background(), audit.SystemActor); err != nil {
		t.Fatal(err)
	}
	validation, err := svc.Validate(context.Background(), "core")
	if err != nil {
		t.Fatal(err)
	}
	if !validation.OK {
		t.Fatalf("validation=%+v", validation)
	}
	act, _, err := svc.Activate(context.Background(), audit.SystemActor, "core")
	if err != nil {
		t.Fatal(err)
	}
	if act.ProfileID != "core" {
		t.Fatalf("act=%+v", act)
	}
}

func TestActivationFailsMissingEntitlement(t *testing.T) {
	svc, lic := openProfiles(t, nil)
	if _, err := lic.AcceptBaseEntitlement(context.Background(), audit.SystemActor); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.Activate(context.Background(), audit.SystemActor, "storage")
	if err == nil {
		t.Fatal("expected entitlement failure for storage profile")
	}
}

func TestActivationFailsMissingBundleArtifact(t *testing.T) {
	svc, lic := openProfiles(t, profiles.ManifestBundleChecker{
		Present: map[string]struct{}{
			"control-plane-image":         {},
			"control-plane-chart":         {},
			"host-agent-image":            {},
			"artifact-server-image":       {},
			"appliance-registry-chart":    {},
			"workspace-provisioner-image": {},
			// workflow-templates intentionally missing (required by workflows/build)
		},
	})
	if _, err := lic.AcceptBaseEntitlement(context.Background(), audit.SystemActor); err != nil {
		t.Fatal(err)
	}
	validation, err := svc.Validate(context.Background(), "builder")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK {
		t.Fatalf("expected bundle failure: %+v", validation)
	}
}
