package zotadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/zotadapter"
)

func TestHTTPClientListRepositories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/_catalog" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"users/alice/app", "library/nginx"}})
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, nil)
	repos, err := client.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos) != 2 {
		t.Errorf("repos = %v, want 2 entries", repos)
	}
}

func TestHTTPClientListTagsEscapesRepositoryName(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "users/alice/app", "tags": []string{"v1", "v2"}})
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, nil)
	tags, err := client.ListTags(t.Context(), "users/alice/app")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2 entries", tags)
	}
	if gotPath != "/v2/users%2Falice%2Fapp/tags/list" {
		t.Errorf("path = %q, want escaped repository segments", gotPath)
	}
}

func TestHTTPClientListReferrers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schemaVersion": 2,
			"manifests": []map[string]any{
				{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:abc", "size": 123, "artifactType": "application/vnd.example.sbom"},
			},
		})
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, nil)
	referrers, err := client.ListReferrers(t.Context(), "library/nginx", "sha256:deadbeef")
	if err != nil {
		t.Fatalf("ListReferrers: %v", err)
	}
	if len(referrers) != 1 || referrers[0].Digest != "sha256:abc" {
		t.Errorf("referrers = %+v", referrers)
	}
}

func TestHTTPClientRequestEditorIsApplied(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{}})
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer internal-credential")
	})
	if _, err := client.ListRepositories(t.Context()); err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if gotAuth != "Bearer internal-credential" {
		t.Errorf("Authorization = %q, want internal credential to be attached", gotAuth)
	}
}

func TestHTTPClientHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, nil)
	if err := client.Health(t.Context()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestHTTPClientHealthFailsOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := zotadapter.NewHTTPClient(srv.URL, nil, nil)
	if err := client.Health(t.Context()); err == nil {
		t.Error("Health should fail on a non-200 status")
	}
}
