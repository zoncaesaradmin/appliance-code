package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

func TestAuditEventsListRequiresPermissionAndPaginates(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	ts.createUserWithRole(t, "viewer", testPassword, roles.ViewerRoleID)
	viewerToken := ts.login(t, "viewer", testPassword)

	for i := 0; i < 3; i++ {
		if err := ts.services.Audit.Record(t.Context(), audit.SystemActor, audit.Event{
			Action:  "test.seed",
			Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("seed audit: %v", err)
		}
	}

	denied := ts.doJSON(t, "GET", "/api/v1/audit/events?limit=2", viewerToken, "")
	defer denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want 403", denied.StatusCode)
	}

	first := ts.doJSON(t, "GET", "/api/v1/audit/events?limit=2", adminToken, "")
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(first.Body)
		t.Fatalf("admin list status = %d body=%s", first.StatusCode, body)
	}
	var page1 struct {
		Items []struct {
			Sequence int64  `json:"sequence"`
			Action   string `json:"action"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(first.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v", page1)
	}

	second := ts.doJSON(t, "GET", "/api/v1/audit/events?limit=2&cursor="+page1.NextCursor, adminToken, "")
	defer second.Body.Close()
	if second.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(second.Body)
		t.Fatalf("page2 status = %d body=%s", second.StatusCode, body)
	}
}
