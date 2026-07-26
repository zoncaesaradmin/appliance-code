// Package landnspublish lets any appliance (base capability) publish a LAN
// A record to a remote landns appliance's DNS records API.
package landnspublish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Request is one outbound publish to a remote DNS appliance.
type Request struct {
	DNSApplianceURL string
	APIToken        string
	Name            string
	IPv4            string
	TTL             int
	Owner           string
}

// Client calls PUT /api/v1/dns/records/{name} on a remote DNS appliance.
type Client struct {
	HTTPClient *http.Client
}

func (c *Client) Publish(ctx context.Context, req Request) error {
	base := strings.TrimRight(strings.TrimSpace(req.DNSApplianceURL), "/")
	token := strings.TrimSpace(req.APIToken)
	name := strings.ToLower(strings.TrimSpace(req.Name))
	ipv4 := strings.TrimSpace(req.IPv4)
	if base == "" {
		return fmt.Errorf("landnspublish: dnsApplianceURL is required")
	}
	if token == "" {
		return fmt.Errorf("landnspublish: apiToken is required")
	}
	if name == "" || strings.Contains(name, ".") {
		return fmt.Errorf("landnspublish: name must be a single DNS label")
	}
	if ip := net.ParseIP(ipv4); ip == nil || ip.To4() == nil {
		return fmt.Errorf("landnspublish: ipv4 must be a valid IPv4 address")
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 60
	}
	body, err := json.Marshal(map[string]any{
		"ipv4":  ipv4,
		"ttl":   ttl,
		"owner": strings.TrimSpace(req.Owner),
	})
	if err != nil {
		return err
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := base + "/api/v1/dns/records/" + url.PathEscape(name)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("landnspublish: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("landnspublish: PUT %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
