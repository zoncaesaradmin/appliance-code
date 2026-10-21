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
