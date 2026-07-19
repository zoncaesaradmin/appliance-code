package sqlite_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

func TestWorkspaceCurrentIsUserScoped(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewWorkspaceStore(db)
	ctx := t.Context()
	ownerA, ownerB := createUser(t, db, "alice"), createUser(t, db, "bob")
	wsA := testWorkspace(ownerA, "alice-ws")
	wsB := testWorkspace(ownerB, "bob-ws")
	if err := store.Create(ctx, wsA); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, wsB); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrent(ctx, ownerA, wsA.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrent(ctx, ownerB, wsB.ID); err != nil {
		t.Fatal(err)
	}
	curA, err := store.GetCurrent(ctx, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	curB, err := store.GetCurrent(ctx, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if curA.WorkspaceID == curB.WorkspaceID {
		t.Fatalf("current workspace leaked across users: %q", curA.WorkspaceID)
	}
}

func TestWorkspaceDeletedHiddenByDefault(t *testing.T) {
	db := openTestDB(t)
	store := sqlite.NewWorkspaceStore(db)
	ctx := t.Context()
	owner := createUser(t, db, "alice")
	ws := testWorkspace(owner, "app")
	if err := store.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDeleted(ctx, ws.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, storage.WorkspaceFilter{OwnerID: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("deleted workspace returned by default: %+v", items)
	}
}

func TestJobAndStepsPersist(t *testing.T) {
	db := openTestDB(t)
	jobs := sqlite.NewJobStore(db)
	ctx := t.Context()
	owner := createUser(t, db, "alice")
	job := storage.Job{ID: uuid.Must(uuid.NewV7()).String(), OwnerID: owner, Type: storage.JobTypeBuild, Status: storage.JobStatusRunning, ArtifactRef: "users/alice/app:v1", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactRef != job.ArtifactRef {
		t.Fatalf("artifactRef = %q, want %q", got.ArtifactRef, job.ArtifactRef)
	}
	step := storage.JobStep{ID: uuid.Must(uuid.NewV7()).String(), JobID: job.ID, Name: "submit", Status: storage.JobStatusRunning}
	if err := jobs.AddStep(ctx, step); err != nil {
		t.Fatal(err)
	}
	steps, err := jobs.ListSteps(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Name != "submit" {
		t.Fatalf("steps = %+v", steps)
	}
}

func createUser(t *testing.T, db *sqlite.DB, username string) string {
	t.Helper()
	store := sqlite.NewUserStore(db)
	id := uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	if err := store.Create(t.Context(), storage.User{ID: id, Username: username, DisplayName: username, State: storage.UserStateActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create user %s: %v", username, err)
	}
	return id
}

func testWorkspace(ownerID, name string) storage.Workspace {
	now := time.Now().UTC()
	return storage.Workspace{ID: uuid.Must(uuid.NewV7()).String(), OwnerID: ownerID, Name: name, WorkProfile: "builder", SourceRepoURL: "https://git.internal.example.com/team/app.git", SourceRef: "0123456789abcdef0123456789abcdef01234567", Status: storage.WorkspaceStatusReady, CreatedAt: now, UpdatedAt: now}
}
