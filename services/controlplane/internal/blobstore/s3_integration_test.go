package blobstore

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const clusterBlobStorageEndpoint = "http://blob-storage.ace-infra.svc.cluster.local:9000"

func TestClusterEndpointPathStyleUploadDownload(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		objects     = map[string][]byte{}
		requestURIs []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestURIs = append(requestURIs, r.RequestURI)
		mu.Unlock()

		if r.URL.Path == "/appliance" && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/appliance/example.txt" {
			switch r.Method {
			case http.MethodPut:
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read upload: %v", err)
				}
				mu.Lock()
				objects[r.URL.Path] = body
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			case http.MethodGet:
				mu.Lock()
				body, ok := objects[r.URL.Path]
				mu.Unlock()
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(body)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	listenerAddress := strings.TrimPrefix(server.URL, "http://")
	var dialed []string
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, listenerAddress)
	}}
	t.Cleanup(transport.CloseIdleConnections)

	client, err := New(clusterBlobStorageEndpoint, "appliance", "access", "secret", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.HTTP = &http.Client{Transport: transport}
	if !client.UsePathStyle {
		t.Fatal("UsePathStyle = false, want true for the in-cluster MinIO endpoint")
	}
	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if _, err := client.Put(context.Background(), "example.txt", strings.NewReader("payload"), int64(len("payload")), "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	response, _, err := client.Get(context.Background(), "example.txt", "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer response.Body.Close()
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(got, []byte("payload")) {
		t.Fatalf("download = %q, want payload", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(dialed) == 0 {
		t.Fatal("no blob-storage connection was attempted")
	}
	for _, address := range dialed {
		if address != "blob-storage.ace-infra.svc.cluster.local:9000" {
			t.Fatalf("resolved %q; want only blob-storage.ace-infra.svc.cluster.local:9000, never appliance.blob-storage…", address)
		}
	}
	for _, requestURI := range requestURIs {
		if !strings.HasPrefix(requestURI, "/appliance") {
			t.Fatalf("request URI = %q, want path-style bucket prefix", requestURI)
		}
	}
}
