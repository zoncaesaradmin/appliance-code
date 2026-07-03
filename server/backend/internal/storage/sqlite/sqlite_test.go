package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/storage/sqlite"
)

func openTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "appliance.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestMigrateAppliesOnceAndRestartsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "appliance.db")

	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	ops := sqlite.NewOperationsStore(db)
	if err := ops.Create(ctx, storage.Operation{
		ID:     "01f0000000000000000000test1",
		Kind:   "test",
		Status: storage.OperationStatusPending,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and migrate again: this must be a no-op that neither errors nor
	// touches previously written data, simulating a process restart.
	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := db2.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	ops2 := sqlite.NewOperationsStore(db2)
	got, err := ops2.Get(ctx, "01f0000000000000000000test1")
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if got.Status != storage.OperationStatusPending {
		t.Errorf("status after restart-migrate = %q, want %q", got.Status, storage.OperationStatusPending)
	}
}

func TestOperationsStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := sqlite.NewOperationsStore(db)

	op := storage.Operation{ID: "op-1", Kind: "artifact.delete", OwnerID: "user-1", Status: storage.OperationStatusPending}
	if err := ops.Create(ctx, op); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ops.Get(ctx, "op-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != op.Kind || got.OwnerID != op.OwnerID || got.Status != op.Status {
		t.Errorf("Get returned %+v, want fields matching %+v", got, op)
	}

	if err := ops.UpdateStatus(ctx, "op-1", storage.OperationStatusSucceeded, []byte(`{"ok":true}`), nil); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err = ops.Get(ctx, "op-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Status != storage.OperationStatusSucceeded {
		t.Errorf("status after update = %q, want succeeded", got.Status)
	}

	if _, err := ops.Get(ctx, "does-not-exist"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
	if err := ops.UpdateStatus(ctx, "does-not-exist", storage.OperationStatusFailed, nil, nil); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("UpdateStatus(missing) error = %v, want ErrNotFound", err)
	}
}

func TestIdempotencyStoreReserveAndComplete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	idem := sqlite.NewIdempotencyStore(db)

	_, claimed, err := idem.Reserve(ctx, "builds.create", "key-1", "hash-1", time.Hour)
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if !claimed {
		t.Fatalf("first Reserve should have claimed the key")
	}

	existing, claimed, err := idem.Reserve(ctx, "builds.create", "key-1", "hash-1", time.Hour)
	if err != nil {
		t.Fatalf("second Reserve: %v", err)
	}
	if claimed {
		t.Fatalf("second Reserve should not re-claim an already-reserved key")
	}
	if existing.RequestHash != "hash-1" {
		t.Errorf("existing.RequestHash = %q, want hash-1", existing.RequestHash)
	}

	if err := idem.Complete(ctx, "builds.create", "key-1", 201, []byte(`{"id":"b1"}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if err := idem.Complete(ctx, "builds.create", "no-such-key", 200, nil); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Complete(missing) error = %v, want ErrNotFound", err)
	}
}

func TestMaintenanceStoreSaveAndGet(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	m := sqlite.NewMaintenanceStore(db)

	if _, err := m.Get(ctx, "token-cleanup"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get(unset) error = %v, want ErrNotFound", err)
	}

	cp := storage.MaintenanceCheckpoint{TaskName: "token-cleanup", Cursor: "cursor-1", LastRunAt: time.Now().UTC()}
	if err := m.Save(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := m.Get(ctx, "token-cleanup")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Cursor != "cursor-1" {
		t.Errorf("Cursor = %q, want cursor-1", got.Cursor)
	}

	cp.Cursor = "cursor-2"
	if err := m.Save(ctx, cp); err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	got, err = m.Get(ctx, "token-cleanup")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Cursor != "cursor-2" {
		t.Errorf("Cursor after update = %q, want cursor-2", got.Cursor)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := sqlite.NewOperationsStore(db)

	sentinel := errors.New("boom")
	err := db.WithTx(ctx, func(ctx context.Context) error {
		if err := ops.Create(ctx, storage.Operation{ID: "tx-1", Kind: "test", Status: storage.OperationStatusPending}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want sentinel", err)
	}

	if _, err := ops.Get(ctx, "tx-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("row should not exist after rollback, Get error = %v", err)
	}
}

func TestBackupProducesRestorableSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := sqlite.NewOperationsStore(db)

	if err := ops.Create(ctx, storage.Operation{ID: "backup-1", Kind: "test", Status: storage.OperationStatusPending}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	restored, err := sqlite.Open(backupPath)
	if err != nil {
		t.Fatalf("Open(backup): %v", err)
	}
	defer restored.Close()

	restoredOps := sqlite.NewOperationsStore(restored)
	got, err := restoredOps.Get(ctx, "backup-1")
	if err != nil {
		t.Fatalf("Get from restored backup: %v", err)
	}
	if got.Kind != "test" {
		t.Errorf("restored operation kind = %q, want test", got.Kind)
	}
}

func TestPing(t *testing.T) {
	db := openTestDB(t)
	if err := db.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
