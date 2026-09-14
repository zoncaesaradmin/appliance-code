package blobstore

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestPathStyleRequestUsesAWSSigV4URIEncoding(t *testing.T) {
	t.Parallel()

	client, err := New(clusterBlobStorageEndpoint, "appliance", "access", "secret", "us-east-1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.Now = func() time.Time {
		return time.Date(2026, time.September, 14, 10, 15, 59, 0, time.UTC)
	}

	request, err := client.newRequest(
		context.Background(),
		http.MethodPut,
		"k3s/v1.30.4+k3s1/k3s",
		nil,
		0,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}

	const expectedPath = "/appliance/k3s/v1.30.4%2Bk3s1/k3s"
	if got := request.URL.EscapedPath(); got != expectedPath {
		t.Fatalf("EscapedPath() = %q, want %q", got, expectedPath)
	}
	if got := request.URL.RequestURI(); got != expectedPath {
		t.Fatalf("RequestURI() = %q, want %q", got, expectedPath)
	}

	const expectedAuthorization = "AWS4-HMAC-SHA256 Credential=access/20260914/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=0b6a00b8a149ea210d9dd8bd89a6d65497573d94d065ef33938cce1cff17244f"
	if got := request.Header.Get("Authorization"); got != expectedAuthorization {
		t.Fatalf("Authorization = %q, want %q", got, expectedAuthorization)
	}
}

func TestAWSCanonicalQueryEncoding(t *testing.T) {
	t.Parallel()

	query := make(map[string][]string)
	query["prefix"] = []string{"k3s/v1.30.4+k3s1/a file"}
	query["delimiter"] = []string{"/"}

	const expected = "delimiter=%2F&prefix=k3s%2Fv1.30.4%2Bk3s1%2Fa%20file"
	if got := awsCanonicalQuery(query); got != expected {
		t.Fatalf("awsCanonicalQuery() = %q, want %q", got, expected)
	}
}
