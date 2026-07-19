package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

func TestDeveloperWorkflowSchemaSupportsWorkspaceAndJobLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "appliance.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) != 1 || migrations[0].Version != 1 {
		t.Fatalf("loadMigrations = %+v, want single baseline migration", migrations)
	}

	now := time.Now().UTC()
	userID := "user-new"
	if err := NewUserStore(db).Create(ctx, storage.User{
		ID: userID, Username: "developer", DisplayName: "Developer", State: storage.UserStateActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	build := storage.Build{
		ID:                 "build-new",
		OwnerID:            userID,
		Status:             storage.BuildStatusRunning,
		SourceRepoURL:      "https://git.internal.example.com/team/app.git",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		ContainerfilePath:  "Containerfile",
		ImageRepository:    "users/alice/app",
		ImageTag:           "0123456789ab",
		BuilderImageDigest: "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkflowName:       "build-new-workflow",
		CreatedAt:          now,
		UpdatedAt:          now,
		DeadlineAt:         now.Add(time.Hour),
	}
	if err := NewBuildStore(db).Create(ctx, build); err != nil {
		t.Fatalf("Create build: %v", err)
	}

	ws := storage.Workspace{
		ID: "workspace-new", OwnerID: userID, Name: "app", WorkProfile: "builder",
		SourceRepoURL: "https://git.internal.example.com/team/app.git", SourceRef: build.SourceCommitSHA,
		Status: storage.WorkspaceStatusReady, CreatedAt: now, UpdatedAt: now,
	}
	workspaces := NewWorkspaceStore(db)
	if err := workspaces.Create(ctx, ws); err != nil {
		t.Fatalf("Create workspace: %v", err)
	}
	if err := workspaces.SetCurrent(ctx, userID, ws.ID); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}

	job := storage.Job{
		ID: "job-new", OwnerID: userID, WorkspaceID: ws.ID, BuildID: build.ID,
		Type: storage.JobTypeBuild, Status: storage.JobStatusRunning, TargetName: "app", ArtifactRef: "users/alice/app:0123456789ab", CreatedAt: now, UpdatedAt: now,
	}
	jobs := NewJobStore(db)
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("Create job: %v", err)
	}
	gotJob, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get job: %v", err)
	}
	if gotJob.ArtifactRef != job.ArtifactRef {
		t.Fatalf("job artifactRef = %q, want %q", gotJob.ArtifactRef, job.ArtifactRef)
	}
	if err := jobs.AddStep(ctx, storage.JobStep{ID: "job-step-new", JobID: job.ID, Name: "submit-build-workflow", Status: storage.JobStatusRunning, CreatedAt: now}); err != nil {
		t.Fatalf("AddStep: %v", err)
	}
}

func TestDeveloperWorkflowSchemaDoesNotPersistWorkflowLogs(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "appliance.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"builds", "jobs", "job_steps"} {
		rows, err := db.sqlDB.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var defaultValue any
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
				t.Fatalf("scanning column for %s: %v", table, err)
			}
			lower := strings.ToLower(name)
			if lower == "logs" || lower == "log" || strings.Contains(lower, "log_text") {
				t.Fatalf("table %s contains workflow log storage column %q", table, name)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating columns for %s: %v", table, err)
		}
	}
}
