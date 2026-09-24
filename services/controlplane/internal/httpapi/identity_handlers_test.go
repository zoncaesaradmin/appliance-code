package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/httpapi"
)

func TestIdentityHandlerDerivesDedicatedChatOrigin(t *testing.T) {
	h := &httpapi.IdentityHandlers{
		ApplianceName:   "zon1",
		DNSZone:         "appliance.internal",
		CanonicalOrigin: "https://zon1.appliance.internal",
	}
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/appliance/identity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		ChatOrigin string `json:"chatOrigin"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ChatOrigin != "https://chat.zon1.appliance.internal" {
		t.Fatalf("chatOrigin = %q", got.ChatOrigin)
	}
}
