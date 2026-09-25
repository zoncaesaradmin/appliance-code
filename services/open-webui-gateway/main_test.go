package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func testGateway(t *testing.T, control http.Handler, upstream http.Handler) gateway {
	t.Helper()
	controlServer := httptest.NewServer(control)
	upstreamServer := httptest.NewServer(upstream)
	t.Cleanup(controlServer.Close)
	t.Cleanup(upstreamServer.Close)
	target, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	return gateway{
		control: controlServer.URL,
		proxy:   httputil.NewSingleHostReverseProxy(target),
		client:  controlServer.Client(),
	}
}

func TestGatewayRequiresAndRevalidatesBridge(t *testing.T) {
	validations := 0
	g := testGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/webui/bridge/validate" {
			http.NotFound(w, r)
			return
		}
		validations++
		_ = json.NewEncoder(w).Encode(bridgeResponse{UserID: "opaque-user", DisplayName: "Alice"})
	}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Appliance-User"); got != "opaque-user" {
			t.Errorf("trusted user header = %q", got)
		}
		if got := r.Header.Get("X-Appliance-Display-Name"); got != "Alice" {
			t.Errorf("trusted display header = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("bridge cookie leaked upstream: %q", got)
		}
		if r.URL.Path != "/api/chats" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	missing := httptest.NewRecorder()
	g.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "https://appliance.invalid/webui/api/chats", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing bridge status = %d", missing.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "https://appliance.invalid/webui/api/chats", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "opaque-bridge"})
	req.Header.Set("X-Appliance-User", "attacker")
	got := httptest.NewRecorder()
	g.ServeHTTP(got, req)
	if got.Code != http.StatusNoContent || validations != 1 {
		t.Fatalf("valid gateway response=%d validations=%d", got.Code, validations)
	}
}

func TestGatewayLaunchUsesPOSTAndStrictCookie(t *testing.T) {
	g := testGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/webui/bridge/consume" || r.Method != http.MethodPost {
			t.Fatalf("unexpected consume request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["grant"] != "one-time" {
			t.Fatalf("unexpected grant body: %#v (%v)", body, err)
		}
		_ = json.NewEncoder(w).Encode(bridgeResponse{Bridge: "bridge-cookie", UserID: "u"})
	}), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "https://appliance.invalid/webui/launch", strings.NewReader("grant=one-time"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	got := httptest.NewRecorder()
	g.ServeHTTP(got, req)
	if got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/webui/" {
		t.Fatalf("launch response %d location=%q", got.Code, got.Header().Get("Location"))
	}
	cookie := got.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/webui" {
		t.Fatalf("unsafe bridge cookie: %#v", cookie)
	}
}

func TestGatewayDoesNotProxyOutsideWebUIPath(t *testing.T) {
	g := testGateway(t, http.NotFoundHandler(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("non-WebUI request reached upstream")
	}))
	got := httptest.NewRecorder()
	g.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "https://appliance.invalid/api/v1/users", nil))
	if got.Code != http.StatusNotFound {
		t.Fatalf("non-WebUI status = %d", got.Code)
	}
}
