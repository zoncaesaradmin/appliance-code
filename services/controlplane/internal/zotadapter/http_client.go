package zotadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

// HTTPClient implements Client against a real zot instance's OCI
// Distribution API (catalog, tags/list, referrers) plus its base health
// route. It carries no identity of its own; the caller supplies whatever
// bearer credential zot's deployment requires, if any, via RequestEditor.
type HTTPClient struct {
	baseURL     string
	httpClient  *http.Client
	requestEdit func(*http.Request) error
}

// NewHTTPClient builds an HTTPClient for the zot instance at baseURL (e.g.
// "http://zot.appliance-registry.svc.cluster.local:5000"). requestEditor,
// if non-nil, is called on every outgoing request to attach whatever
// internal credential this zot deployment requires.
func NewHTTPClient(baseURL string, hc *http.Client, requestEditor func(*http.Request) error) *HTTPClient {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPClient{baseURL: strings.TrimSuffix(baseURL, "/"), httpClient: hc, requestEdit: requestEditor}
}

func (c *HTTPClient) request(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("zotadapter: building request for %s: %w", path, err)
	}
	traceCtx, traceID := ctxutil.EnsureTraceID(req.Context())
	req = req.WithContext(traceCtx)
	req.Header.Set(ctxutil.TraceIDHeader, traceID)
	if c.requestEdit != nil {
		if err := c.requestEdit(req); err != nil {
			return nil, fmt.Errorf("zotadapter: authorizing %s: %w", path, err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zotadapter: calling %s: %w", path, err)
	}
	return resp, nil
}

func (c *HTTPClient) get(ctx context.Context, path string, out any) error {
	resp, err := c.request(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("zotadapter: %s returned status %d", path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("zotadapter: decoding response from %s: %w", path, err)
	}
	return nil
}

func (c *HTTPClient) ListRepositories(ctx context.Context) ([]string, error) {
	var result struct {
		Repositories []string `json:"repositories"`
	}
	if err := c.get(ctx, "/v2/_catalog", &result); err != nil {
		return nil, err
	}
	return result.Repositories, nil
}

func (c *HTTPClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	var result struct {
		Tags []string `json:"tags"`
	}
	path := "/v2/" + url.PathEscape(repository) + "/tags/list"
	if err := c.get(ctx, path, &result); err != nil {
		return nil, err
	}
	return result.Tags, nil
}

func (c *HTTPClient) ListReferrers(ctx context.Context, repository, digest string) ([]Descriptor, error) {
	var index struct {
		Manifests []Descriptor `json:"manifests"`
	}
	path := "/v2/" + url.PathEscape(repository) + "/referrers/" + url.PathEscape(digest)
	if err := c.get(ctx, path, &index); err != nil {
		return nil, err
	}
	return index.Manifests, nil
}

func (c *HTTPClient) Health(ctx context.Context) error {
	resp, err := c.request(ctx, "/v2/")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	return fmt.Errorf("zotadapter: /v2/ returned status %d", resp.StatusCode)
}
