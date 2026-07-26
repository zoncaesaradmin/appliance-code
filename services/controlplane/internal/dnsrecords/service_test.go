package dnsrecords_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/dnsrecords"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func openDNSTest(t *testing.T) (*dnsrecords.Service, *dnsrecords.MemoryZoneSyncer, storage.DNSRecordStore) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "dns.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := sqlite.NewDNSRecordStore(db)
	syncer := &dnsrecords.MemoryZoneSyncer{}
	svc := dnsrecords.NewService(store, db, syncer, audit.NewRecorder(sqlite.NewAuditStore(db)), dnsrecords.Config{
		Zone:              "appliance.internal",
		BootstrapHostname: "dns1",
		BootstrapIPv4:     "192.0.2.10",
		PeerLease:         time.Minute,
	})
	return svc, syncer, store
}

func TestUpsertAdminAndPeerOwnership(t *testing.T) {
	svc, syncer, _ := openDNSTest(t)
	ctx := context.Background()
	if err := svc.BootstrapSelf(ctx); err != nil {
		t.Fatalf("BootstrapSelf: %v", err)
	}
	if !strings.Contains(syncer.Last, "dns1 300 IN A 192.0.2.10") {
		t.Fatalf("zone missing bootstrap record:\n%s", syncer.Last)
	}

	rec, err := svc.Upsert(ctx, dnsrecords.UpsertInput{
		Name: "registry1", IPv4: "192.0.2.20", AsAdmin: true, Actor: audit.SystemActor,
	})
	if err != nil {
		t.Fatalf("admin upsert: %v", err)
	}
	if rec.Source != storage.DNSRecordSourceAdmin {
		t.Fatalf("source = %s", rec.Source)
	}
	if !strings.Contains(syncer.Last, "registry1 300 IN A 192.0.2.20") {
		t.Fatalf("zone missing admin record:\n%s", syncer.Last)
	}

	_, err = svc.Upsert(ctx, dnsrecords.UpsertInput{
		Name: "registry1", IPv4: "192.0.2.21", Owner: "peer-a", Actor: audit.SystemActor,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("peer overwrite of admin = %v, want conflict", err)
	}

	_, err = svc.Upsert(ctx, dnsrecords.UpsertInput{
		Name: "builder1", IPv4: "192.0.2.30", Owner: "peer-b", Actor: audit.SystemActor,
	})
	if err != nil {
		t.Fatalf("peer create: %v", err)
	}
	_, err = svc.Upsert(ctx, dnsrecords.UpsertInput{
		Name: "builder1", IPv4: "192.0.2.31", Owner: "peer-b", Actor: audit.SystemActor,
	})
	if err != nil {
		t.Fatalf("peer renew: %v", err)
	}
	_, err = svc.Upsert(ctx, dnsrecords.UpsertInput{
		Name: "builder1", IPv4: "192.0.2.32", Owner: "peer-c", Actor: audit.SystemActor,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("foreign peer overwrite = %v, want conflict", err)
	}
}

func TestPeerLeaseExpiry(t *testing.T) {
	svc, _, store := openDNSTest(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Minute)
	if err := store.Upsert(ctx, storage.DNSRecord{
		Name: "stale", IPv4: "192.0.2.40", TTL: 60, Source: storage.DNSRecordSourcePeer,
		Owner: "peer", CreatedAt: past, UpdatedAt: past, LeaseExpiresAt: &past,
	}); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	n, err := svc.ExpireStale(ctx)
	if err != nil {
		t.Fatalf("ExpireStale: %v", err)
	}
	if n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	if _, err := store.Get(ctx, "stale"); err != storage.ErrNotFound {
		t.Fatalf("stale still present: %v", err)
	}
}
