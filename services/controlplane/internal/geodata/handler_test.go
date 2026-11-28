package geodata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct{ saved SaveRequest }

func (s *fakeStore) List(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[{"name":"test"}]`), nil
}
func (s *fakeStore) Save(_ context.Context, r SaveRequest) (string, error) {
	s.saved = r
	return "zone-id", nil
}
func (s *fakeStore) Ready(context.Context) error { return nil }

func TestEditorEndpointsRequireInternalAuthentication(t *testing.T) {
	store := &fakeStore{}
	h, err := NewHandler(store, "test-internal-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "wrong", http.StatusUnauthorized},
		{"valid", "test-internal-token", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/zones?order=name.asc", nil)
			req.Header.Set(InternalAuthHeader, tc.token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geo/editor/rpc/save_zone", strings.NewReader(`{"p_name":"zone","p_description":"","p_geometry":{"type":"Polygon","coordinates":[]},"p_id":null,"p_revision":null}`))
	req.Header.Set(InternalAuthHeader, "test-internal-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `"zone-id"` || store.saved.Name != "zone" {
		t.Fatalf("status %d body %q request %+v", rec.Code, rec.Body.String(), store.saved)
	}
}
