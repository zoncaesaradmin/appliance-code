package metadatabundle_test

import (
	"context"
	"encoding/json"
	"testing"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/auditops"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func TestInvokeAutomationExportsAuditEvents(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	auditStore := sqlite.NewAuditStore(db)
	recorder := audit.NewRecorder(auditStore)
	auditOps, err := auditops.NewService(auditStore, sqlite.NewOperationsStore(db), dir, 90, logger)
	if err != nil {
		t.Fatalf("audit ops: %v", err)
	}
	svc, err := metadatabundle.NewService(db, sqlite.NewMetadataBundleStore(db), recorder, auditOps, dir)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	result, err := svc.InvokeAutomation(context.Background(), audit.SystemActor, "zon.debug-tools:export-audit-events", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("InvokeAutomation: %v", err)
	}
	if result.AutomationID != "zon.debug-tools:export-audit-events" {
		t.Fatalf("automation id = %q", result.AutomationID)
	}
	if result.DocumentVersion != "1.0.0" {
		t.Fatalf("document version = %q", result.DocumentVersion)
	}
	if result.Output["exportId"] == "" {
		t.Fatalf("expected exportId in output: %#v", result.Output)
	}
	if got := result.Output["status"]; got != string(storage.OperationStatusPending) {
		t.Fatalf("status = %#v, want %q", got, storage.OperationStatusPending)
	}
}

func TestInvokeAutomationRejectsUnexpectedInput(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging: %v", err)
	}
	auditStore := sqlite.NewAuditStore(db)
	recorder := audit.NewRecorder(auditStore)
	auditOps, err := auditops.NewService(auditStore, sqlite.NewOperationsStore(db), dir, 90, logger)
	if err != nil {
		t.Fatalf("audit ops: %v", err)
	}
	svc, err := metadatabundle.NewService(db, sqlite.NewMetadataBundleStore(db), recorder, auditOps, dir)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	if _, err := svc.InvokeAutomation(context.Background(), audit.SystemActor, "zon.debug-tools:export-audit-events", json.RawMessage(`{"unexpected":true}`)); err == nil {
		t.Fatal("expected input validation error")
	}
}
