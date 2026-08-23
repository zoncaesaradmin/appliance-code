package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/roles"
)

func TestVideoLibraryUploadListAndStream(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileTraining)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createRoleResp := ts.doJSON(t, "POST", "/api/v1/roles", adminToken, `{"name":"video-editor","permissions":["video.library.read","video.library.write","video.play"]}`)
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

	ts.createUserWithRole(t, "video-user", testPassword, createdRole.ID)
	token := ts.login(t, "video-user", testPassword)

	uploadReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/video/library/clips/intro.mp4", strings.NewReader("fake-mp4-bytes"))
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

	uploadedPath := filepath.Join(ts.videoLibraryRoot, "clips", "intro.mp4")
	if _, err := os.Stat(uploadedPath); err != nil {
		t.Fatalf("stat uploaded video: %v", err)
	}

	listResp := ts.doJSON(t, "GET", "/api/v1/video/library/clips", token, "")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}

	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/video/library/clips/intro.mp4", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", streamResp.StatusCode)
	}
	if got := streamResp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	body, err := io.ReadAll(streamResp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if string(body) != "fake-mp4-bytes" {
		t.Fatalf("stream body = %q", string(body))
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/video/library/clips/intro.mp4", nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleteResp.StatusCode)
	}
}

func TestVideoLibraryAbsentOnCoreProfile(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	resp := ts.doJSON(t, "GET", "/api/v1/video/library", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("core video library status = %d, want 404", resp.StatusCode)
	}
}

func TestVideoLibraryRequiresWritePermission(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileTraining)
	ts.bootstrapAdmin(t, "admin", testPassword)
	_ = ts.login(t, "admin", testPassword)

	ts.createUserWithRole(t, "viewer", testPassword, roles.ViewerRoleID)
	viewerToken := ts.login(t, "viewer", testPassword)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/video/library/clip.mp4", strings.NewReader("denied"))
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
}
