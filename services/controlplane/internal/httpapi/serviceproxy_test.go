package httpapi

import "testing"

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
