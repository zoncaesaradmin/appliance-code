package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"appliance-code/services/controlplane/internal/logging"
)

// NewAIProxyHandler exposes the selected engine's OpenAI-compatible /v1
// surface behind the stable appliance /ai prefix. Engine-native administrative
// paths are deliberately outside this proxy and remain internal-only.
func NewAIProxyHandler(logger logging.Logger, baseURL string) (http.Handler, error) {
	if logger == nil {
		return nil, fmt.Errorf("AI proxy logger is required")
	}
	target, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || target.Scheme == "" || target.Host == "" || target.Path != "" {
		return nil, fmt.Errorf("AI proxy base URL must be an absolute URL with no path")
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Appliance credentials terminate at the control plane and must never
			// be disclosed to the engine container.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
			path := strings.TrimPrefix(pr.In.URL.Path, "/ai")
			if path == "" {
				path = "/"
			}
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.WithContext(r.Context()).Warnw("AI gateway call failed", "method", r.Method, "path", r.URL.Path, "error", err)
			WriteProblem(w, r, http.StatusBadGateway, "inference_unavailable", "The inference runtime is unavailable", "")
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ai/v1/") && r.URL.Path != "/ai/v1" {
			WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
			return
		}
		w.Header().Set("X-Accel-Buffering", "no")
		proxy.ServeHTTP(w, r)
	}), nil
}
