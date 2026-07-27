package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/roles"
)

func TestArtifactFilesUploadAndDownload(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createRoleResp := ts.doJSON(t, "POST", "/api/v1/roles", adminToken, `{"name":"artifact-writer","permissions":["artifacts.read","artifacts.write"]}`)
	defer createRoleResp.Body.Close()
	if createRoleResp.StatusCode != http.StatusCreated {
		t.Fatalf("create role status = %d, want 201", createRoleResp.StatusCode)
	}
	var createdRole struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createRoleResp.Body).Decode(&createdRole); err != nil {
		t.Fatalf("decode role response: %v", err)
	}

	ts.createUserWithRole(t, "artifact-user", testPassword, createdRole.ID)
	token := ts.login(t, "artifact-user", testPassword)

	uploadReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files/releases/v1/bundle.txt", strings.NewReader("hello from appliance"))
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	uploadReq.Header.Set("Authorization", "Bearer "+token)
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", uploadResp.StatusCode)
	}

	downloadReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/files/releases/v1/bundle.txt", nil)
	if err != nil {
		t.Fatalf("build download request: %v", err)
	}
	downloadReq.Header.Set("Authorization", "Bearer "+token)
	downloadResp, err := http.DefaultClient.Do(downloadReq)
	if err != nil {
		t.Fatalf("download request: %v", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", downloadResp.StatusCode)
	}
	body, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	if string(body) != "hello from appliance" {
		t.Fatalf("download body = %q, want %q", string(body), "hello from appliance")
	}
}

func TestArtifactFilesRequireWritePermission(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	ts.createUserWithRole(t, "viewer", testPassword, roles.ViewerRoleID)
	viewerToken := ts.login(t, "viewer", testPassword)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/files/releases/v1/bundle.txt", strings.NewReader("denied"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("upload status = %d, want 403", resp.StatusCode)
	}

	getResp := ts.doJSON(t, "GET", "/api/v1/files/releases/v1/bundle.txt", token, "")
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file read status = %d, want 404", getResp.StatusCode)
	}
}
