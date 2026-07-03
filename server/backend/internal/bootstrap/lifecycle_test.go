package bootstrap_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/authn"
	"appliance-code/server/backend/internal/authz"
	"appliance-code/server/backend/internal/bootstrap"
	"appliance-code/server/backend/internal/keys"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/storage/sqlite"
	"appliance-code/server/backend/internal/tokens"
	"appliance-code/server/backend/internal/users"
)

type harness struct {
	db       *sqlite.DB
	users    *users.Service
	roles    *roles.Service
	tokens   *tokens.Service
	sessions *authn.SessionService
	authz    *authz.Service
	audit    storage.AuditStore
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

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

	keyMaterial, err := keys.LoadOrGenerate(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatalf("keys.LoadOrGenerate: %v", err)
	}

	auditStore := sqlite.NewAuditStore(db)
	recorder := audit.NewRecorder(auditStore)

	userStore := sqlite.NewUserStore(db)
	tokenStore := sqlite.NewTokenStore(db)
	sessionStore := sqlite.NewSessionStore(db)
	throttleStore := sqlite.NewThrottleStore(db)

	usersSvc := users.NewService(db, userStore, roleStore, tokenStore, sessionStore, throttleStore, recorder, keyMaterial)
	rolesSvc := roles.NewService(db, roleStore, userStore, recorder)
	tokensSvc := tokens.NewService(db, tokenStore, recorder, keyMaterial)
	sessionSvc := authn.NewSessionService(db, userStore, sessionStore, throttleStore, recorder, keyMaterial, "https://appliance.local", "appliance-api")
	authzSvc := authz.NewService(roleStore)

	return &harness{db: db, users: usersSvc, roles: rolesSvc, tokens: tokensSvc, sessions: sessionSvc, authz: authzSvc, audit: auditStore}
}

func systemActor() audit.Actor {
	return audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
}

func TestBootstrapCreatesAdministrator(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	result, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	if result.Username != "admin" {
		t.Errorf("Username = %q, want admin", result.Username)
	}

	perms, err := h.authz.EffectivePermissions(ctx, result.AdminUserID)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if !perms[roles.PermUsersCreate] || !perms[roles.PermSystemOperate] {
		t.Errorf("administrator should have full permissions, got %v", perms)
	}
}

func TestBootstrapRefusesSecondCall(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	userStore := sqlite.NewUserStore(h.db)
	roleStore := sqlite.NewRoleStore(h.db)

	if _, err := bootstrap.Init(ctx, h.db, userStore, roleStore, h.users, "admin", "a-very-long-bootstrap-password", "Administrator"); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if _, err := bootstrap.Init(ctx, h.db, userStore, roleStore, h.users, "admin2", "another-long-password-here", "Second"); !errors.Is(err, bootstrap.ErrAlreadyInitialized) {
		t.Errorf("second Init error = %v, want ErrAlreadyInitialized", err)
	}
}

func TestLoginRefreshLogoutLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	result, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}

	login, err := h.sessions.Login(ctx, "127.0.0.1", "req-1", "admin", "a-very-long-bootstrap-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if login.AccessToken == "" || login.RefreshToken == "" {
		t.Fatal("Login should return both an access token and a refresh token")
	}

	user, claims, err := h.sessions.ValidateAccessToken(ctx, login.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if user.ID != result.AdminUserID || claims.FamilyID != login.FamilyID {
		t.Errorf("validated token doesn't match issued session: user=%+v claims=%+v", user, claims)
	}

	refreshed, err := h.sessions.Refresh(ctx, "127.0.0.1", "req-2", login.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.FamilyID != login.FamilyID {
		t.Errorf("refresh should keep the same family id")
	}

	// Reusing the original (now rotated-out) refresh token must be rejected
	// and must revoke the whole family.
	if _, err := h.sessions.Refresh(ctx, "127.0.0.1", "req-3", login.RefreshToken); !errors.Is(err, authn.ErrInvalidRefreshToken) {
		t.Errorf("reused refresh token error = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := h.sessions.Refresh(ctx, "127.0.0.1", "req-4", refreshed.RefreshToken); !errors.Is(err, authn.ErrInvalidRefreshToken) {
		t.Errorf("refresh after reuse-triggered revocation should also fail, got %v", err)
	}

	// A fresh login should still work after the previous family was revoked.
	login2, err := h.sessions.Login(ctx, "127.0.0.1", "req-5", "admin", "a-very-long-bootstrap-password")
	if err != nil {
		t.Fatalf("second Login: %v", err)
	}
	if err := h.sessions.Logout(ctx, audit.Actor{UserID: result.AdminUserID, Type: storage.AuditActorUser}, login2.FamilyID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := h.sessions.Refresh(ctx, "127.0.0.1", "req-6", login2.RefreshToken); !errors.Is(err, authn.ErrInvalidRefreshToken) {
		t.Errorf("refresh after logout should fail, got %v", err)
	}
}

func TestLoginRejectsWrongPasswordAndUnknownUser(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}

	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-1", "admin", "wrong-password-entirely"); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Errorf("wrong password error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-2", "nonexistent-user", "whatever-password-here"); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Errorf("unknown user error = %v, want ErrInvalidCredentials", err)
	}
}

func TestDisableRevokesSessionsAndTokens(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	admin, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	dev, err := h.users.Create(ctx, systemActor(), "developer", "Dev", "a-very-long-developer-password")
	if err != nil {
		t.Fatalf("creating developer: %v", err)
	}
	if err := h.roles.SetUserRoles(ctx, systemActor(), dev.ID, []string{roles.DeveloperRoleID}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}

	login, err := h.sessions.Login(ctx, "127.0.0.1", "req-1", "developer", "a-very-long-developer-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	_, tok, err := h.tokens.Create(ctx, systemActor(), dev.ID, "ci-token", 0, nil)
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}

	if err := h.users.Disable(ctx, audit.Actor{UserID: admin.AdminUserID, Type: storage.AuditActorUser}, dev.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, _, err := h.sessions.ValidateAccessToken(ctx, login.AccessToken); err == nil {
		t.Error("access token should be rejected after disable (credential version bump)")
	}
	if _, err := h.sessions.Refresh(ctx, "127.0.0.1", "req-2", login.RefreshToken); !errors.Is(err, authn.ErrInvalidRefreshToken) {
		t.Errorf("refresh after disable error = %v, want ErrInvalidRefreshToken", err)
	}

	got, err := h.tokens.Get(ctx, tok.ID)
	if err != nil {
		t.Fatalf("Get token: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("api token should be revoked after user disable")
	}
}

func TestLastAdministratorInvariant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}

	if err := h.users.Disable(ctx, systemActor(), admin.AdminUserID); !errors.Is(err, users.ErrLastAdministrator) {
		t.Errorf("disabling the last administrator error = %v, want ErrLastAdministrator", err)
	}
	if err := h.roles.SetUserRoles(ctx, systemActor(), admin.AdminUserID, []string{roles.ViewerRoleID}); !errors.Is(err, roles.ErrLastAdministrator) {
		t.Errorf("demoting the last administrator error = %v, want ErrLastAdministrator", err)
	}

	// Adding a second administrator must make both operations succeed now.
	second, err := h.users.Create(ctx, systemActor(), "admin2", "Second Admin", "another-long-admin-password")
	if err != nil {
		t.Fatalf("creating second admin: %v", err)
	}
	if err := h.roles.SetUserRoles(ctx, systemActor(), second.ID, []string{roles.AdministratorRoleID}); err != nil {
		t.Fatalf("promoting second admin: %v", err)
	}
	if err := h.users.Disable(ctx, systemActor(), admin.AdminUserID); err != nil {
		t.Errorf("disabling the first admin should now succeed: %v", err)
	}
}

func TestPasswordResetInvalidatesSessionsAndSetsNewPassword(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}

	login, err := h.sessions.Login(ctx, "127.0.0.1", "req-1", "admin", "a-very-long-bootstrap-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	raw, err := h.users.InitiatePasswordReset(ctx, systemActor(), admin.AdminUserID)
	if err != nil {
		t.Fatalf("InitiatePasswordReset: %v", err)
	}

	if err := h.users.CompletePasswordReset(ctx, raw, "brand-new-long-password-1"); err != nil {
		t.Fatalf("CompletePasswordReset: %v", err)
	}
	// Single-use: replaying the same credential must fail.
	if err := h.users.CompletePasswordReset(ctx, raw, "another-new-long-password"); !errors.Is(err, users.ErrInvalidResetCredential) {
		t.Errorf("replayed reset credential error = %v, want ErrInvalidResetCredential", err)
	}

	if _, _, err := h.sessions.ValidateAccessToken(ctx, login.AccessToken); err == nil {
		t.Error("prior session should be invalidated after password reset")
	}

	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-2", "admin", "a-very-long-bootstrap-password"); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Errorf("old password should no longer work, error = %v", err)
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-3", "admin", "brand-new-long-password-1"); err != nil {
		t.Errorf("new password should work: %v", err)
	}
}

func TestAPITokenCreateAuthenticateRevoke(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator")
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}

	raw, tok, err := h.tokens.Create(ctx, systemActor(), admin.AdminUserID, "test-token", 0, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tok.ExpiresAt.Sub(tok.CreatedAt) != tokens.DefaultLifetime {
		t.Errorf("default lifetime = %v, want %v", tok.ExpiresAt.Sub(tok.CreatedAt), tokens.DefaultLifetime)
	}

	authenticated, err := h.tokens.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if authenticated.ID != tok.ID {
		t.Errorf("authenticated token id = %s, want %s", authenticated.ID, tok.ID)
	}

	if err := h.tokens.Revoke(ctx, systemActor(), tok.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := h.tokens.Authenticate(ctx, raw); !errors.Is(err, tokens.ErrInvalidToken) {
		t.Errorf("authenticating a revoked token error = %v, want ErrInvalidToken", err)
	}

	if _, _, err := h.tokens.Create(ctx, systemActor(), admin.AdminUserID, "bad-ttl", time.Minute, nil); !errors.Is(err, tokens.ErrInvalidLifetime) {
		t.Errorf("creating a token below MinLifetime error = %v, want ErrInvalidLifetime", err)
	}
}

func TestAuditChainVerifies(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := bootstrap.Init(ctx, h.db, sqlite.NewUserStore(h.db), sqlite.NewRoleStore(h.db), h.users, "admin", "a-very-long-bootstrap-password", "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-1", "admin", "a-very-long-bootstrap-password"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := h.sessions.Login(ctx, "127.0.0.1", "req-2", "admin", "wrong-password"); err == nil {
		t.Fatal("expected login failure")
	}

	if err := h.audit.VerifyChain(ctx); err != nil {
		t.Errorf("VerifyChain: %v", err)
	}
}
