package licensing_test

import (
	"context"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func openLicensing(t *testing.T) (*licensing.Service, storage.LicensingStore) {
	t.Helper()
	db, err := sqlite.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := sqlite.NewLicensingStore(db)
	svc := licensing.NewService(db, store, audit.NewRecorder(sqlite.NewAuditStore(db)))
	return svc, store
}

func TestLicensingStartsUnresolved(t *testing.T) {
	svc, _ := openLicensing(t)
	st, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Resolved || st.State != storage.LicensingUnresolved {
		t.Fatalf("got %+v", st)
	}
}

func TestAcceptBaseEntitlement(t *testing.T) {
	svc, _ := openLicensing(t)
	st, err := svc.AcceptBaseEntitlement(context.Background(), audit.SystemActor)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Resolved || st.State != storage.LicensingBaseFree {
		t.Fatalf("got %+v", st)
	}
	if len(st.EntitledCapabilities) == 0 {
		t.Fatal("expected base capabilities")
	}
}

func TestImportLicenseRejectsBadSignature(t *testing.T) {
	svc, _ := openLicensing(t)
	_, err := svc.ImportLicense(context.Background(), audit.SystemActor, `{
		"version":1,"issuer":"zon","capabilities":["base"],"signature":"bad"
	}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestImportLicenseAcceptsOfflineDev(t *testing.T) {
	svc, _ := openLicensing(t)
	doc := `{
		"version":1,
		"issuer":"zon",
		"capabilities":["base","host","workflows","artifact"],
		"signature":"offline-dev",
		"validFrom":"2020-01-01T00:00:00Z",
		"validTo":"2099-01-01T00:00:00Z"
	}`
	st, err := svc.ImportLicense(context.Background(), audit.SystemActor, doc)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != storage.LicensingLicensed {
		t.Fatalf("state=%s", st.State)
	}
	ok, err := svc.IsCapabilityEntitled(context.Background(), "artifact")
	if err != nil || !ok {
		t.Fatalf("artifact entitled=%v err=%v", ok, err)
	}
}

func TestParseRejectsExpired(t *testing.T) {
	_, err := licensing.ParseAndValidateDocument(`{
		"version":1,"issuer":"zon","capabilities":["base"],"signature":"offline-dev",
		"validTo":"2020-01-01T00:00:00Z"
	}`)
	if err == nil {
		t.Fatal("expected expired error")
	}
	_ = time.Now
}
