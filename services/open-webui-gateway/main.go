package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const cookieName = "zon_webui_bridge"

type bridgeResponse struct{ Bridge, UserID, DisplayName string }

func main() {
	listen := requiredEnv("LISTEN_ADDRESS")
	upstream, err := url.Parse(requiredEnv("UPSTREAM_URL"))
	if err != nil {
		log.Fatal(err)
	}
	control := strings.TrimRight(requiredEnv("CONTROL_PLANE_URL"), "/")
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("upstream error: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
	}
	h := gateway{control: control, proxy: proxy, client: &http.Client{Timeout: 10 * time.Second}}
	server := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func requiredEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		log.Fatalf("%s is required", name)
	}
	return v
}

type gateway struct {
	control string
	proxy   *httputil.ReverseProxy
	client  *http.Client
}

func (g gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path == "/webui/launch" && r.Method == http.MethodPost {
		g.launch(w, r)
		return
	}
	if r.URL.Path != "/webui" && !strings.HasPrefix(r.URL.Path, "/webui/") {
		http.NotFound(w, r)
		return
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		http.Error(w, "workspace session required", http.StatusUnauthorized)
		return
	}
	identity, ok := g.validate(r.Context(), c.Value)
	if !ok {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/webui", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Error(w, "workspace session expired", http.StatusUnauthorized)
		return
	}
	g.prepareUpstreamRequest(r, identity)
	if isWebSocket(r) {
		g.proxyWebSocket(w, r, c.Value)
		return
	}
	g.proxy.ServeHTTP(w, r)
}

func (g gateway) prepareUpstreamRequest(r *http.Request, identity bridgeResponse) {
	// Never pass caller-controlled trusted identity headers or a bridge cookie
	// upstream. Open WebUI only receives identity asserted after validation.
	r.Header.Del("X-Appliance-User")
	r.Header.Del("X-Appliance-Display-Name")
	r.Header.Del("X-Appliance-Role")
	r.Header.Set("X-Appliance-User", identity.UserID)
	r.Header.Set("X-Appliance-Display-Name", identity.DisplayName)
	r.Header.Set("X-Appliance-Role", "user")
	r.Header.Del("Cookie")
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/webui")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
}

func (g gateway) launch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid workspace launch", http.StatusBadRequest)
		return
	}
	grant := strings.TrimSpace(r.PostForm.Get("grant"))
	if grant == "" {
		http.Error(w, "invalid workspace launch", http.StatusBadRequest)
		return
	}
	body, _ := json.Marshal(map[string]string{"grant": grant})
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, g.control+"/api/v1/webui/bridge/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		http.Error(w, "invalid workspace launch", http.StatusUnauthorized)
		return
	}
	defer resp.Body.Close()
	var identity bridgeResponse
	if json.NewDecoder(resp.Body).Decode(&identity) != nil || identity.Bridge == "" {
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: identity.Bridge, Path: "/webui", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int((8 * time.Hour).Seconds())})
	http.Redirect(w, r, "/webui/", http.StatusSeeOther)
}

func (g gateway) validate(ctx context.Context, bridge string) (bridgeResponse, bool) {
	body, _ := json.Marshal(map[string]string{"bridge": bridge})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, g.control+"/api/v1/webui/bridge/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return bridgeResponse{}, false
	}
	defer resp.Body.Close()
	var identity bridgeResponse
	return identity, json.NewDecoder(resp.Body).Decode(&identity) == nil && identity.UserID != ""
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") && strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// proxyWebSocket keeps a raw upgraded connection only while the bridge remains
// valid. Unlike the standard reverse proxy's blind stream, this periodically
// asks the control plane to recheck user state, session-family revocation, and
// inference.use; revocation closes both ends immediately.
func (g gateway) proxyWebSocket(w http.ResponseWriter, r *http.Request, bridge string) {
	target := g.proxy.Director
	copyRequest := r.Clone(r.Context())
	target(copyRequest)
	upstream, err := net.DialTimeout("tcp", copyRequest.URL.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if buffered.Reader.Buffered() != 0 {
		return
	}
	copyRequest.Host = copyRequest.URL.Host
	if err := copyRequest.Write(upstream); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if _, ok := g.validate(context.Background(), bridge); !ok {
				return
			}
		}
	}
}
