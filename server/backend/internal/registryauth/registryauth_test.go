package registryauth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"appliance-code/server/backend/internal/registryauth"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
)

func TestNormalizeRepositoryName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Users/Alice/My-App", "users/alice/my-app", false},
		{"", "", true},
		{"/leading/slash", "", true},
		{"trailing/slash/", "", true},
		{"a//b", "", true},
		{"../escape", "", true},
		{"weird$char", "", true},
		{"valid.name_1/sub-repo", "valid.name_1/sub-repo", false},
	}
	for _, c := range cases {
		got, err := registryauth.NormalizeRepositoryName(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("NormalizeRepositoryName(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("NormalizeRepositoryName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseScopes(t *testing.T) {
	scopes, err := registryauth.ParseScopes([]string{"repository:Users/Alice/App:pull,push"})
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	if len(scopes) != 1 || scopes[0].Name != "users/alice/app" {
		t.Fatalf("unexpected scopes: %+v", scopes)
	}
	if len(scopes[0].Actions) != 2 {
		t.Errorf("actions = %v, want [pull push]", scopes[0].Actions)
	}

	badCases := []string{
		"repository:name",
		"repository:name:",
		"repository::pull",
		"image:name:pull",
		"repository:name:delete",
	}
	for _, s := range badCases {
		if _, err := registryauth.ParseScopes([]string{s}); err == nil {
			t.Errorf("ParseScopes(%q) should have failed", s)
		}
	}
}

// fakeRoleLister and fakeGrantStore let grant-authorization tests run
// without a real database.
type fakeRoleLister struct {
	rolesByUser map[string][]storage.Role
}

func (f *fakeRoleLister) ListUserRoles(_ context.Context, userID string) ([]storage.Role, error) {
	return f.rolesByUser[userID], nil
}

type fakeGrantStore struct {
	grants []storage.RegistryGrant
}

func (f *fakeGrantStore) Create(_ context.Context, g storage.RegistryGrant) error {
	f.grants = append(f.grants, g)
	return nil
}
func (f *fakeGrantStore) Get(_ context.Context, id string) (storage.RegistryGrant, error) {
	for _, g := range f.grants {
		if g.ID == id {
			return g, nil
		}
	}
	return storage.RegistryGrant{}, storage.ErrNotFound
}
func (f *fakeGrantStore) List(_ context.Context) ([]storage.RegistryGrant, error) {
	return f.grants, nil
}
func (f *fakeGrantStore) ListForSubjects(_ context.Context, userID string, roleIDs []string) ([]storage.RegistryGrant, error) {
	var out []storage.RegistryGrant
	for _, g := range f.grants {
		if g.SubjectType == storage.RegistryGrantSubjectUser && g.SubjectID == userID {
			out = append(out, g)
			continue
		}
		if g.SubjectType == storage.RegistryGrantSubjectRole {
			for _, r := range roleIDs {
				if g.SubjectID == r {
					out = append(out, g)
					break
				}
			}
		}
	}
	return out, nil
}
func (f *fakeGrantStore) Delete(_ context.Context, id string) error {
	for i, g := range f.grants {
		if g.ID == id {
			f.grants = append(f.grants[:i], f.grants[i+1:]...)
			return nil
		}
	}
	return storage.ErrNotFound
}

func TestAuthorizeAdministratorGetsEverything(t *testing.T) {
	roleLister := &fakeRoleLister{rolesByUser: map[string][]storage.Role{
		"admin-1": {{ID: roles.AdministratorRoleID, Name: roles.Administrator}},
	}}
	authz := registryauth.NewAuthorizer(&fakeGrantStore{}, roleLister)

	perms := map[string]bool{roles.PermRegistryPull: true, roles.PermRegistryPush: true}
	requests, err := registryauth.ParseScopes([]string{"repository:anything/at/all:pull,push"})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := authz.Authorize(context.Background(), "admin-1", "admin", perms, requests)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if len(decisions) != 1 || len(decisions[0].Granted) != 2 {
		t.Errorf("administrator should be granted both actions on any repository, got %+v", decisions)
	}
}

func TestAuthorizeDeveloperOwnPrefixOnly(t *testing.T) {
	roleLister := &fakeRoleLister{rolesByUser: map[string][]storage.Role{
		"dev-1": {{ID: roles.DeveloperRoleID, Name: roles.Developer}},
	}}
	authz := registryauth.NewAuthorizer(&fakeGrantStore{}, roleLister)
	perms := map[string]bool{roles.PermRegistryPull: true, roles.PermRegistryPush: true}

	// Push to own prefix: allowed.
	ownReq, _ := registryauth.ParseScopes([]string{"repository:users/alice/app:pull,push"})
	decisions, err := authz.Authorize(context.Background(), "dev-1", "alice", perms, ownReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions[0].Granted) != 2 {
		t.Errorf("developer should get pull+push on their own users/ prefix, got %v", decisions[0].Granted)
	}

	// Push to someone else's prefix: denied; pull anywhere: allowed.
	otherReq, _ := registryauth.ParseScopes([]string{"repository:users/bob/app:pull,push"})
	decisions, err = authz.Authorize(context.Background(), "dev-1", "alice", perms, otherReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions[0].Granted) != 1 || decisions[0].Granted[0] != "pull" {
		t.Errorf("developer should only get pull on another user's prefix, got %v", decisions[0].Granted)
	}
}

func TestAuthorizeAutomationRequiresExplicitGrant(t *testing.T) {
	roleLister := &fakeRoleLister{rolesByUser: map[string][]storage.Role{
		"auto-1": {{ID: roles.AutomationRoleID, Name: roles.Automation}},
	}}
	grantStore := &fakeGrantStore{}
	authz := registryauth.NewAuthorizer(grantStore, roleLister)
	perms := map[string]bool{roles.PermRegistryPull: true, roles.PermRegistryPush: true}

	req, _ := registryauth.ParseScopes([]string{"repository:ci/pipeline-a:pull,push"})
	decisions, err := authz.Authorize(context.Background(), "auto-1", "ci-bot", perms, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions[0].Granted) != 0 {
		t.Errorf("automation should get no implicit grants, got %v", decisions[0].Granted)
	}

	grantStore.grants = append(grantStore.grants, storage.RegistryGrant{
		ID: "g1", SubjectType: storage.RegistryGrantSubjectUser, SubjectID: "auto-1",
		PathPrefix: "ci/pipeline-a", Actions: []string{"pull", "push"},
	})
	decisions, err = authz.Authorize(context.Background(), "auto-1", "ci-bot", perms, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions[0].Granted) != 2 {
		t.Errorf("automation with an explicit grant should get both actions, got %v", decisions[0].Granted)
	}
}

func TestAuthorizeDeniesWithoutBasePermission(t *testing.T) {
	roleLister := &fakeRoleLister{rolesByUser: map[string][]storage.Role{
		"admin-1": {{ID: roles.AdministratorRoleID, Name: roles.Administrator}},
	}}
	authz := registryauth.NewAuthorizer(&fakeGrantStore{}, roleLister)

	// No registry.push permission at all (e.g. an API token scoped to pull
	// only): push must be denied even for an administrator's own account.
	perms := map[string]bool{roles.PermRegistryPull: true}
	req, _ := registryauth.ParseScopes([]string{"repository:anything:pull,push"})
	decisions, err := authz.Authorize(context.Background(), "admin-1", "admin", perms, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions[0].Granted) != 1 || decisions[0].Granted[0] != "pull" {
		t.Errorf("push should be denied without the base registry.push permission, got %v", decisions[0].Granted)
	}
}

func TestIssueTokenProducesVerifiableSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	token, expiresAt, err := registryauth.IssueToken(priv, "kid-1", "https://appliance.local", "user-1", "zot", "jti-1", []registryauth.AccessEntry{
		{Type: "repository", Name: "users/alice/app", Actions: []string{"pull", "push"}},
	})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if time.Until(expiresAt) > registryauth.TokenLifetime || time.Until(expiresAt) <= 0 {
		t.Errorf("expiresAt = %v, want within TokenLifetime from now", expiresAt)
	}

	parts := splitJWT(t, token)
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), decodeSig(t, parts[2])) {
		t.Error("token signature should verify against the corresponding public key")
	}

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if ed25519.Verify(otherPub, []byte(parts[0]+"."+parts[1]), decodeSig(t, parts[2])) {
		t.Error("token signature should not verify against an unrelated public key")
	}
}

func splitJWT(t *testing.T, token string) []string {
	t.Helper()
	var parts []string
	start := 0
	for i, c := range token {
		if c == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	parts = append(parts, token[start:])
	if len(parts) != 3 {
		t.Fatalf("token %q does not have 3 dot-separated parts", token)
	}
	return parts
}

func decodeSig(t *testing.T, b64Value string) []byte {
	t.Helper()
	sig, err := base64.RawURLEncoding.DecodeString(b64Value)
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	return sig
}
