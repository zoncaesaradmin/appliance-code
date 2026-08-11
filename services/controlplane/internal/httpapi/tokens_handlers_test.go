package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAPITokenCreateListAndRevokeHidesRevoked(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createResp := ts.doJSON(t, "POST", "/api/v1/tokens", adminToken, `{"name":"artifact-client","lifetimeSeconds":3600}`)
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.Token == "" {
		t.Fatalf("create response missing id/token: %+v", created)
	}

	listResp := ts.doJSON(t, "GET", "/api/v1/tokens", adminToken, "")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	var listed struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("list after create = %+v, want the created token", listed.Items)
	}

	revokeResp := ts.doJSON(t, "DELETE", "/api/v1/tokens/"+created.ID, adminToken, "")
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", revokeResp.StatusCode)
	}

	afterResp := ts.doJSON(t, "GET", "/api/v1/tokens", adminToken, "")
	defer afterResp.Body.Close()
	if afterResp.StatusCode != http.StatusOK {
		t.Fatalf("list after revoke status = %d, want 200", afterResp.StatusCode)
	}
	var after struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(afterResp.Body).Decode(&after); err != nil {
		t.Fatalf("decode list after revoke: %v", err)
	}
	if len(after.Items) != 0 {
		t.Fatalf("list after revoke = %+v, want empty active token list", after.Items)
	}
}
