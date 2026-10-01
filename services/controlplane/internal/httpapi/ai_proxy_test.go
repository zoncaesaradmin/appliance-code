package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zoncaesaradmin/platformkit/ctxutil"

	"appliance-code/services/controlplane/internal/logging"
)

func TestAIProxyStripsOnlyAIPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "stream=true" {
			t.Fatalf("upstream URL=%s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("appliance credentials reached upstream")
		}
		if r.Header.Get("X-Appliance-User-Id") != "" {
			t.Fatalf("caller-controlled identity reached upstream")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	logger, _ := logging.New("error")
	handler, err := NewAIProxyHandler(logger, upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/ai/v1/chat/completions?stream=true", nil)
	req.Header.Set("Authorization", "Bearer appliance-secret")
	req.Header.Set("Cookie", "appliance_session=secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestAIProxyRejectsOutsidePrefix(t *testing.T) {
	logger, _ := logging.New("error")
	handler, err := NewAIProxyHandler(logger, "http://inference.invalid")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/delete", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestAIProxyInjectsAuthenticatedIdentity(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Appliance-User-Id"); got != "user-1" {
			t.Fatalf("user header = %q", got)
		}
		if got := r.Header.Get("X-Appliance-Request-Id"); got != "request-1" {
			t.Fatalf("request header = %q", got)
		}
	}))
	defer upstream.Close()
	logger, _ := logging.New("error")
	handler, err := NewAIProxyHandler(logger, upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/ai/v1/chat/completions", nil)
	req.Header.Set("X-Appliance-User-Id", "attacker")
	ctx := context.WithValue(req.Context(), principalCtxKey{}, Principal{UserID: "user-1"})
	req = req.WithContext(ctxutil.WithRequestID(ctx, "request-1"))
	handler.ServeHTTP(httptest.NewRecorder(), req)
}
