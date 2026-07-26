package landnspublish_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/landnspublish"
)

func TestPublish_CallsRemoteDNSRecordsAPI(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"registry1"}`))
	}))
	t.Cleanup(srv.Close)

	client := &landnspublish.Client{HTTPClient: srv.Client()}
	err := client.Publish(context.Background(), landnspublish.Request{
		DNSApplianceURL: srv.URL,
		APIToken:        "tok-1",
		Name:            "Registry1",
		IPv4:            "192.0.2.20",
		TTL:             60,
		Owner:           "node-2",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotPath != "/api/v1/dns/records/registry1" {
		t.Fatalf("path = %q", gotPath)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["ipv4"] != "192.0.2.20" || payload["owner"] != "node-2" {
		t.Fatalf("body = %#v", payload)
	}
}

func TestPublish_RejectsInvalidInput(t *testing.T) {
	client := &landnspublish.Client{}
	err := client.Publish(context.Background(), landnspublish.Request{
		DNSApplianceURL: "https://dns.example",
		APIToken:        "tok",
		Name:            "registry1.appliance.internal",
		IPv4:            "192.0.2.20",
	})
	if err == nil {
		t.Fatal("expected error for FQDN name")
	}
}
