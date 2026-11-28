package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/logging"
)

func TestGeoWildcardProxyPreservesPathAndAuthorization(t *testing.T) {
	var gotPath, gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	logger, err := logging.New("error")
	if err != nil {
		t.Fatal(err)
	}
	route := ServiceProxyRoute{Method: "GET", ExternalPath: "/api/v1/geo/{path...}", UpstreamPath: "/api/v1/geo/{path...}", Permission: "geo.read"}
	handler, err := newServiceProxyHandler(logger, nil, ServiceProxyRegistration{Name: "geo-data-api", BaseURL: upstream.URL}, route)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(proxyPattern(route), handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/editor/rpc/save_zone?order=name.asc", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalCtxKey{}, Principal{UserID: "u1", Permissions: map[string]bool{"geo.read": true}}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || gotPath != "/api/v1/geo/editor/rpc/save_zone" || gotQuery != "order=name.asc" {
		t.Fatalf("status=%d path=%q query=%q", rec.Code, gotPath, gotQuery)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/geo/zones", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalCtxKey{}, Principal{UserID: "u2", Permissions: map[string]bool{}}))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized permission status=%d, want 403", rec.Code)
	}
}

func TestStripProxyPrefix(t *testing.T) {
	cases := []struct {
		path, prefix, want string
	}{
		{"/inference/v1/responses", "/inference", "/v1/responses"},
		{"/inference/v1/models", "/inference", "/v1/models"},
		{"/inference/v1/chat/completions", "/inference", "/v1/chat/completions"},
		{"/inference", "/inference", "/"},
		{"/inference/", "/inference", "/"},
		{"/other/v1/models", "/inference", "/other/v1/models"},
	}
	for _, tc := range cases {
		if got := stripProxyPrefix(tc.path, tc.prefix); got != tc.want {
			t.Fatalf("stripProxyPrefix(%q, %q) = %q, want %q", tc.path, tc.prefix, got, tc.want)
		}
	}
}

func TestProxyPattern(t *testing.T) {
	if got := proxyPattern(ServiceProxyRoute{Method: "ANY", ExternalPath: "/inference/{path...}"}); got != "/inference/{path...}" {
		t.Fatalf("ANY pattern = %q", got)
	}
	if got := proxyPattern(ServiceProxyRoute{Method: "GET", ExternalPath: "/api/v1/host/info"}); got != "GET /api/v1/host/info" {
		t.Fatalf("GET pattern = %q", got)
	}
}
