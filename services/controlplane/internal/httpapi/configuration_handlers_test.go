package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestConfigurationRevisionCaptureAndImport(t *testing.T) {
	source := newTestServer(t)
	source.bootstrapAdmin(t, "admin", testPassword)
	adminToken := source.login(t, "admin", testPassword)

	captured := source.doJSON(t, http.MethodPost, "/api/v1/configuration/revisions", adminToken, "{}")
	defer captured.Body.Close()
	if captured.StatusCode != http.StatusCreated {
		t.Fatalf("capture status = %d, want %d", captured.StatusCode, http.StatusCreated)
	}
	var revision struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(captured.Body).Decode(&revision); err != nil {
		t.Fatalf("decode captured revision: %v", err)
	}
	if !json.Valid(revision.Payload) {
		t.Fatalf("captured payload is not JSON: %q", revision.Payload)
	}
	archive := source.doJSON(t, http.MethodPost, "/api/v1/configuration/archives", adminToken, "{}")
	defer archive.Body.Close()
	if archive.StatusCode != http.StatusCreated {
		t.Fatalf("archive status = %d, want %d", archive.StatusCode, http.StatusCreated)
	}
	var archiveBody struct {
		ArchiveID string `json:"archiveId"`
	}
	if err := json.NewDecoder(archive.Body).Decode(&archiveBody); err != nil {
		t.Fatalf("decode archive response: %v", err)
	}
	if archiveBody.ArchiveID == "" {
		t.Fatal("archive response omitted archiveId")
	}
	listed := source.doJSON(t, http.MethodGet, "/api/v1/configuration/archives", adminToken, "")
	defer listed.Body.Close()
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("archive list status = %d, want %d", listed.StatusCode, http.StatusOK)
	}
	verified := source.doJSON(t, http.MethodGet, "/api/v1/configuration/archives/"+archiveBody.ArchiveID+"/verify", adminToken, "")
	defer verified.Body.Close()
	if verified.StatusCode != http.StatusOK {
		t.Fatalf("archive verify status = %d, want %d", verified.StatusCode, http.StatusOK)
	}
	downloaded := source.doJSON(t, http.MethodGet, "/api/v1/configuration/archives/"+archiveBody.ArchiveID+"/download", adminToken, "")
	defer downloaded.Body.Close()
	if downloaded.StatusCode != http.StatusOK {
		t.Fatalf("archive download status = %d, want %d", downloaded.StatusCode, http.StatusOK)
	}
	if downloaded.Header.Get("Content-Type") != "application/vnd.zon.configuration-archive+json" {
		t.Fatalf("archive content type = %q", downloaded.Header.Get("Content-Type"))
	}
	var envelope map[string]any
	if err := json.NewDecoder(downloaded.Body).Decode(&envelope); err != nil || envelope["ciphertext"] == nil || envelope["manifest"] == nil {
		t.Fatalf("downloaded archive is not a sealed envelope: %#v, %v", envelope, err)
	}

	target := newTestServer(t)
	target.bootstrapAdmin(t, "admin", testPassword)
	targetToken := target.login(t, "admin", testPassword)
	request, err := json.Marshal(map[string]json.RawMessage{"payload": revision.Payload})
	if err != nil {
		t.Fatalf("encode import request: %v", err)
	}
	imported := target.doJSON(t, http.MethodPost, "/api/v1/configuration/revisions/import", targetToken, string(request))
	defer imported.Body.Close()
	if imported.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d, want %d", imported.StatusCode, http.StatusCreated)
	}

	conflict := target.doJSON(t, http.MethodPost, "/api/v1/configuration/revisions/import", targetToken, string(request))
	defer conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("second import status = %d, want %d", conflict.StatusCode, http.StatusConflict)
	}
}
