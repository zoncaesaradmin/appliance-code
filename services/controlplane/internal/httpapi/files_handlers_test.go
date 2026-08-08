package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/roles"
)

func TestArtifactFilesUploadAndDownload(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createRoleResp := ts.doJSON(t, "POST", "/api/v1/roles", adminToken, `{"name":"file-writer","permissions":["files.read","files.write"]}`)
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

	uploadedPath := filepath.Join(ts.filesRoot, "releases", "v1", "bundle.txt")
	info, err := os.Stat(uploadedPath)
	if err != nil {
		t.Fatalf("stat uploaded file: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("uploaded file mode = %o, want 644", info.Mode().Perm())
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

	listResp := ts.doJSON(t, "GET", "/api/v1/files", token, "")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list root status = %d, want 200", listResp.StatusCode)
	}
	var rootList struct {
		Path  string `json:"path"`
		Items []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&rootList); err != nil {
		t.Fatalf("decode root list: %v", err)
	}
	if len(rootList.Items) != 1 || rootList.Items[0].Name != "releases" || rootList.Items[0].Type != "directory" {
		t.Fatalf("root list = %+v, want releases directory", rootList.Items)
	}

	nestedResp := ts.doJSON(t, "GET", "/api/v1/files/releases/v1", token, "")
	defer nestedResp.Body.Close()
	if nestedResp.StatusCode != http.StatusOK {
		t.Fatalf("list nested status = %d, want 200", nestedResp.StatusCode)
	}
	var nestedList struct {
		Items []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.NewDecoder(nestedResp.Body).Decode(&nestedList); err != nil {
		t.Fatalf("decode nested list: %v", err)
	}
	if len(nestedList.Items) != 1 || nestedList.Items[0].Name != "bundle.txt" || nestedList.Items[0].Type != "file" {
		t.Fatalf("nested list = %+v, want bundle.txt file", nestedList.Items)
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
