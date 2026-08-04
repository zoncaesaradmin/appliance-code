package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"appliance-code/services/controlplane/internal/roles"
)

func TestLicensingAndProfileFlow(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	statusResp := ts.doJSON(t, "GET", "/api/v1/licensing/status", adminToken, "")
	defer statusResp.Body.Close()
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("status code=%d", statusResp.StatusCode)
	}
	var status map[string]any
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["resolved"] != false {
		t.Fatalf("expected unresolved: %#v", status)
	}

	notesResp := ts.doJSON(t, "GET", "/api/v1/notifications", adminToken, "")
	defer notesResp.Body.Close()
	if notesResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(notesResp.Body)
		t.Fatalf("notifications code=%d body=%s", notesResp.StatusCode, body)
	}
	var notes struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(notesResp.Body).Decode(&notes); err != nil {
		t.Fatal(err)
	}
	if len(notes.Items) == 0 {
		t.Fatal("expected licensing notification")
	}

	acceptResp := ts.doJSON(t, "POST", "/api/v1/licensing/base-entitlement/accept", adminToken, "")
	defer acceptResp.Body.Close()
	if acceptResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(acceptResp.Body)
		t.Fatalf("accept code=%d body=%s", acceptResp.StatusCode, body)
	}

	createResp := ts.doJSON(t, "GET", "/api/v1/appliance/profiles", adminToken, "")
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("list profiles code=%d body=%s", createResp.StatusCode, body)
	}

	activateResp := ts.doJSON(t, "POST", "/api/v1/appliance/profiles/core/activate", adminToken, "")
	defer activateResp.Body.Close()
	if activateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(activateResp.Body)
		t.Fatalf("activate code=%d body=%s", activateResp.StatusCode, body)
	}

	metadataResp := ts.doJSON(t, "GET", "/api/v1/appliance/metadata-bundle", adminToken, "")
	defer metadataResp.Body.Close()
	if metadataResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(metadataResp.Body)
		t.Fatalf("metadata status code=%d body=%s", metadataResp.StatusCode, body)
	}

	ts.createUserWithRole(t, "viewer-user", testPassword, roles.ViewerRoleID)
	viewerToken := ts.login(t, "viewer-user", testPassword)
	denied := ts.doJSON(t, "POST", "/api/v1/licensing/base-entitlement/accept", viewerToken, "")
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer accept code=%d want 403", denied.StatusCode)
	}
}
