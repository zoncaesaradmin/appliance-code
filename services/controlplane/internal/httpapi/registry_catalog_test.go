package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/zotadapter"
)

func (ts *testServer) fakeZot(t *testing.T) *zotadapter.Fake {
	t.Helper()
	fake, ok := ts.services.Zot.(*zotadapter.Fake)
	if !ok {
		t.Fatalf("services.Zot is %T, want *zotadapter.Fake in tests", ts.services.Zot)
	}
	return fake
}

func TestListRepositoriesFiltersByGrant(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)

	fake := ts.fakeZot(t)
	fake.Tags["users/alice/app"] = []string{"v1"}
	fake.Tags["users/bob/app"] = []string{"v1"}
	fake.Tags["library/nginx"] = []string{"latest"}

	aliceToken := ts.login(t, "alice", testPassword)
	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories", aliceToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Alice (developer) can pull everything (implicit "" prefix), so all
	// three repositories should be visible to her.
	if len(body.Items) != 3 {
		t.Errorf("alice's visible repositories = %v, want 3", body.Items)
	}
}

func TestListRepositoriesEmptyForCallerWithoutPullPermission(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "automation-user", testPassword, roles.AutomationRoleID)

	fake := ts.fakeZot(t)
	fake.Tags["library/nginx"] = []string{"latest"}

	token := ts.login(t, "automation-user", testPassword)
	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories", token, "")
	defer resp.Body.Close()
	var body struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Errorf("automation with no grants should see no repositories, got %v", body.Items)
	}
}

func TestListTagsForPullableRepository(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	fake := ts.fakeZot(t)
	fake.Tags["library/nginx"] = []string{"1.0", "1.1"}

	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories/library/nginx/tags", adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Errorf("tags = %v, want 2 entries", body.Items)
	}
}

func TestListTagsDeniedForUnpullableRepository(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "automation-user", testPassword, roles.AutomationRoleID)

	fake := ts.fakeZot(t)
	fake.Tags["ci/pipeline-a"] = []string{"1.0"}

	token := ts.login(t, "automation-user", testPassword)
	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories/ci/pipeline-a/tags", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (not 403, to avoid confirming existence)", resp.StatusCode)
	}
}

func TestListTagsReturns404ForNonexistentRepository(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	// The fake zot has no entry for this repository at all, distinct from
	// "caller not allowed to see it" — administrator can pull anything, so
	// this must surface as a clean 404, not a 502 registry-unavailable.
	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories/library/does-not-exist/tags", adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status for a nonexistent repository = %d, want 404", resp.StatusCode)
	}
}

func TestListReferrersRequiresDigestQueryParam(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	fake := ts.fakeZot(t)
	fake.Tags["library/nginx"] = []string{"1.0"}

	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories/library/nginx/referrers", adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status without digest = %d, want 400", resp.StatusCode)
	}
}

func TestListReferrersReturnsDescriptors(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	fake := ts.fakeZot(t)
	fake.Tags["library/nginx"] = []string{"1.0"}
	fake.Referrers["library/nginx@sha256:abc"] = []zotadapter.Descriptor{
		{MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: "sha256:def", Size: 42, ArtifactType: "application/vnd.example.sbom"},
	}

	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories/library/nginx/referrers?digest=sha256:abc", adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			Digest string `json:"digest"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].Digest != "sha256:def" {
		t.Errorf("referrers = %+v", body.Items)
	}
}
