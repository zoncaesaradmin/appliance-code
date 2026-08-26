package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
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

	emptyListResp := ts.doJSON(t, "GET", "/api/v1/video/library", token, "")
	defer emptyListResp.Body.Close()
	if emptyListResp.StatusCode != http.StatusOK {
		t.Fatalf("empty library status = %d, want 200", emptyListResp.StatusCode)
	}
	if !ts.videoStore.hasBucket() {
		t.Fatal("listing an empty video library must initialize the blob bucket")
	}

	validVideo := browserMP4("avc1")
	uploadReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/video/library/clips/intro.mp4", strings.NewReader(string(validVideo)))
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	uploadReq.Header.Set("Authorization", "Bearer "+token)
	uploadReq.Header.Set("Content-Type", "video/mp4")
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", uploadResp.StatusCode)
	}
	var uploadResult struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&uploadResult); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResult.Status != "ready" {
		t.Fatalf("upload status = %q, want ready", uploadResult.Status)
	}

	if !ts.videoStore.has("appliance/video/library/clips/intro.mp4") {
		t.Fatal("uploaded video was not stored below the configured blob prefix")
	}

	playbackSessionResp := ts.doJSONWithHeaders(t, "POST", "/api/v1/video/playback-session", token, "", map[string]string{
		"X-Forwarded-Proto": "https",
	})
	defer playbackSessionResp.Body.Close()
	if playbackSessionResp.StatusCode != http.StatusNoContent {
		t.Fatalf("prepare playback status = %d, want 204", playbackSessionResp.StatusCode)
	}
	var playbackCookie *http.Cookie
	for _, cookie := range playbackSessionResp.Cookies() {
		if cookie.Name == "appliance_video_playback" {
			playbackCookie = cookie
			break
		}
	}
	if playbackCookie == nil {
		t.Fatal("prepare playback did not set the playback cookie")
	}
	if !playbackCookie.HttpOnly || !playbackCookie.Secure || playbackCookie.Path != "/api/v1/video/stream/" || playbackCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("playback cookie is not scoped securely: %+v", playbackCookie)
	}

	listResp := ts.doJSON(t, "GET", "/api/v1/video/library/clips", token, "")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}

	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/video/stream/clips/intro.mp4", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	streamReq.Header.Set("Range", "bytes=0-15")
	streamReq.AddCookie(playbackCookie)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("stream status = %d, want 206", streamResp.StatusCode)
	}
	if got := streamResp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	if got := streamResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "video/mp4") {
		t.Fatalf("Content-Type = %q, want video/mp4", got)
	}
	if got := streamResp.Header.Get("Content-Range"); !strings.HasPrefix(got, "bytes 0-15/") {
		t.Fatalf("Content-Range = %q, want bytes 0-15/...", got)
	}
	body, err := io.ReadAll(streamResp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if string(body) != string(validVideo[:16]) {
		t.Fatalf("stream body does not match requested byte range")
	}

	unauthenticatedStreamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/video/stream/clips/intro.mp4", nil)
	if err != nil {
		t.Fatalf("build unauthenticated stream request: %v", err)
	}
	unauthenticatedStreamResp, err := http.DefaultClient.Do(unauthenticatedStreamReq)
	if err != nil {
		t.Fatalf("unauthenticated stream request: %v", err)
	}
	defer unauthenticatedStreamResp.Body.Close()
	if unauthenticatedStreamResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stream status = %d, want 401", unauthenticatedStreamResp.StatusCode)
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

func TestVideoLibraryRejectsInvalidOrUnsupportedMP4(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileTraining)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	for _, testCase := range []struct {
		name string
		path string
		body string
	}{
		{name: "wrong extension", path: "clip.webm", body: string(browserMP4("avc1"))},
		{name: "invalid bytes", path: "clip.mp4", body: "not an MP4"},
		{name: "unsupported video codec", path: "clip.mp4", body: string(browserMP4("hvc1"))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/video/library/"+testCase.path, strings.NewReader(testCase.body))
			if err != nil {
				t.Fatalf("build upload request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "video/mp4")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("upload request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("upload status = %d, want 400", resp.StatusCode)
			}
		})
	}
	if ts.videoStore.has("appliance/video/library/clip.mp4") {
		t.Fatal("invalid video was stored")
	}
}

func browserMP4(videoCodec string) []byte {
	fileType := mp4TestBox("ftyp", append([]byte("isom\x00\x00\x00\x00"), []byte("isomiso2avc1mp41")...))
	sampleEntry := mp4TestBox(videoCodec, make([]byte, 8))
	sampleDescription := mp4TestBox("stsd", append(append(make([]byte, 4), []byte{0, 0, 0, 1}...), sampleEntry...))
	mediaHeader := make([]byte, 12)
	copy(mediaHeader[8:], []byte("vide"))
	media := mp4TestBox("mdia", append(mp4TestBox("hdlr", mediaHeader), mp4TestBox("minf", mp4TestBox("stbl", sampleDescription))...))
	return append(fileType, mp4TestBox("moov", mp4TestBox("trak", media))...)
}

func mp4TestBox(kind string, payload []byte) []byte {
	box := make([]byte, 8+len(payload))
	box[0] = byte(len(box) >> 24)
	box[1] = byte(len(box) >> 16)
	box[2] = byte(len(box) >> 8)
	box[3] = byte(len(box))
	copy(box[4:8], kind)
	copy(box[8:], payload)
	return box
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
