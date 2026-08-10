package builds_test

import (
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/buildergit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/tokens"
	"appliance-code/services/controlplane/internal/users"
	"appliance-code/services/controlplane/internal/workflows"
)

func TestCreateProvisionsRegistryPushCredentials(t *testing.T) {
	ctx := t.Context()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "appliance.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	roleStore := sqlite.NewRoleStore(db)
	if err := roles.Seed(ctx, roleStore); err != nil {
		t.Fatalf("roles.Seed: %v", err)
	}
	keyMaterial, err := keys.LoadOrGenerate(t.TempDir())
	if err != nil {
		t.Fatalf("keys.LoadOrGenerate: %v", err)
	}
	recorder := audit.NewRecorder(sqlite.NewAuditStore(db))
	userStore := sqlite.NewUserStore(db)
	tokenStore := sqlite.NewTokenStore(db)
	usersSvc := users.NewService(db, userStore, roleStore, tokenStore, sqlite.NewSessionStore(db), sqlite.NewThrottleStore(db), recorder, keyMaterial)
	tokensSvc := tokens.NewService(db, tokenStore, recorder, keyMaterial)
	user, err := usersSvc.Create(ctx, systemActor(), "alice", "Alice", "Correct-Horse-Battery-Staple-9")
	if err != nil {
		t.Fatalf("users.Create: %v", err)
	}

	fake := workflows.NewFake()
	secrets := buildergit.NewMemorySecretManager()
	svc := builds.NewService(db, sqlite.NewBuildStore(db), sqlite.NewIdempotencyStore(db), fake, recorder,
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, time.Hour,
		"/data/zon/workspaces", "controlplane-workspaces", nil)
	if err := svc.ConfigureRegistryPush(builds.RegistryPushConfig{
		Host:      "test-device-1.appliance.internal",
		TLSVerify: "false",
		Namespace: "appliance-builds",
		Secrets:   secrets,
		Tokens:    tokensSvc,
		Users:     usersSvc,
	}); err != nil {
		t.Fatalf("ConfigureRegistryPush: %v", err)
	}

	req := validRequest()
	req.Execution = "make"
	req.Args = []string{"image"}
	req.WorkingDirectory = "services/controlplane"
	req.ImageRepository = "appliance-images/appliance-control-plane"

	build, err := svc.Create(ctx, systemActor(), user.ID, req, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if build.PushTokenID == "" || build.RegistrySecretName == "" {
		t.Fatalf("expected push credential bookkeeping, got token=%q secret=%q", build.PushTokenID, build.RegistrySecretName)
	}
	spec, ok := fake.SubmittedSpec(build.WorkflowName)
	if !ok {
		t.Fatal("submitted workflow spec missing")
	}
	if spec.RegistryHost != "test-device-1.appliance.internal" {
		t.Fatalf("RegistryHost = %q", spec.RegistryHost)
	}
	if spec.RegistryCredentialSecret != build.RegistrySecretName {
		t.Fatalf("RegistryCredentialSecret = %q, want %q", spec.RegistryCredentialSecret, build.RegistrySecretName)
	}
	if spec.RegistryTLSVerify != "false" {
		t.Fatalf("RegistryTLSVerify = %q", spec.RegistryTLSVerify)
	}
	secret, found, err := secrets.Get(ctx, "appliance-builds", build.RegistrySecretName)
	if err != nil || !found {
		t.Fatalf("registry secret Get: found=%v err=%v", found, err)
	}
	if secret.Data["username"] != "alice" || secret.Data["token"] == "" {
		t.Fatalf("secret data = %#v", secret.Data)
	}

	fake.SetStatus(build.WorkflowName, workflows.Status{Phase: workflows.PhaseSucceeded})
	got, err := svc.Get(ctx, build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != storage.BuildStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
	if _, found, err := secrets.Get(ctx, "appliance-builds", build.RegistrySecretName); err != nil || found {
		t.Fatalf("secret should be deleted after success: found=%v err=%v", found, err)
	}
	if _, err := tokensSvc.Get(ctx, build.PushTokenID); err == nil {
		// Token record may still exist but must be revoked.
		tok, getErr := tokenStore.Get(ctx, build.PushTokenID)
		if getErr != nil {
			t.Fatalf("tokenStore.Get: %v", getErr)
		}
		if tok.RevokedAt == nil {
			t.Fatal("push token should be revoked after build success")
		}
	}
}

func TestRegistryHostFromOrigin(t *testing.T) {
	host, err := builds.RegistryHostFromOrigin("https://test-device-1.appliance.internal")
	if err != nil {
		t.Fatal(err)
	}
	if host != "test-device-1.appliance.internal" {
		t.Fatalf("host = %q", host)
	}
	if _, err := builds.RegistryHostFromOrigin("not-a-url"); err == nil {
		t.Fatal("expected error for invalid origin")
	}
}
