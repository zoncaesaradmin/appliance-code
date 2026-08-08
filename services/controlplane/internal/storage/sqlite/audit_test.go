package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func TestAuditListCursorAndCheckpointRetention(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := sqlite.NewAuditStore(db)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := store.Append(ctx, storage.AuditEvent{
			ID:         fmt.Sprintf("evt-%d", i+1),
			OccurredAt: base.Add(time.Duration(i) * time.Hour),
			ActorType:  storage.AuditActorSystem,
			Action:     "test.action",
			Outcome:    storage.AuditOutcomeSuccess,
			Severity:   storage.AuditSeverityInfo,
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	page1, err := store.List(ctx, storage.AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != 2 || page1[0].Sequence != 5 || page1[1].Sequence != 4 {
		t.Fatalf("page1 = %+v", page1)
	}
	page2, err := store.List(ctx, storage.AuditFilter{Limit: 2, BeforeSequence: page1[1].Sequence})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 2 || page2[0].Sequence != 3 {
		t.Fatalf("page2 = %+v", page2)
	}

	if err := store.VerifyChain(ctx); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	seq, hash, err := store.LatestSequence(ctx)
	if err != nil || seq != 5 || len(hash) == 0 {
		t.Fatalf("LatestSequence = %d %v %v", seq, hash, err)
	}
	if err := store.CreateCheckpoint(ctx, storage.AuditCheckpoint{
		ID: "cp-1", CreatedAt: time.Now().UTC(), LastSequence: seq, ChainHash: hash,
	}); err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	deleted, err := store.DeleteOlderThan(ctx, base.Add(90*time.Minute), seq)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	if err := store.VerifyChain(ctx); err != nil {
		t.Fatalf("VerifyChain after prune: %v", err)
	}
}
