package identityclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{1,62}$`)

// Principal is the provider-neutral identity returned by application identity.
// It intentionally has no WhatsApp or other provider fields.
type Principal struct {
	ApplicationUserID string    `json:"application_user_id"`
	TenantID          string    `json:"tenant_id"`
	ChannelID         string    `json:"channel_id"`
	Audience          string    `json:"audience"`
	SubjectID         string    `json:"subject_id,omitempty"`
	SessionID         string    `json:"session_id,omitempty"`
	AuthMethod        string    `json:"auth_method"`
	ExpiresAt         time.Time `json:"expires_at"`
	Roles             []string  `json:"roles"`
	Permissions       []string  `json:"permissions"`
	ZoneScopes        []string  `json:"zone_scopes"`
	DiscoveryURL      string    `json:"discovery_url,omitempty"`
}

type Resolver interface {
	Resolve(context.Context, string, string) (Principal, error)
}

type Client struct {
	endpoint, key, tenantID, channelID string
	httpClient                         *http.Client
}

func New(endpoint, key, tenantID, channelID string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("application identity URL is invalid")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(decoded) < 32 {
		return nil, errors.New("application identity key is invalid")
	}
	if !idPattern.MatchString(tenantID) || !idPattern.MatchString(channelID) {
		return nil, errors.New("application identity tenant or channel is invalid")
	}
	return &Client{endpoint: strings.TrimRight(parsed.String(), "/") + "/internal/v1/application-principal/resolve", key: strings.TrimSpace(key), tenantID: tenantID, channelID: channelID, httpClient: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (c *Client) Resolve(ctx context.Context, subjectID, phone string) (Principal, error) {
	if c == nil || strings.TrimSpace(subjectID) == "" || !phonePattern.MatchString(phone) {
		return Principal{}, errors.New("invalid application identity subject")
	}
	payload, err := json.Marshal(struct {
		TenantID  string `json:"tenant_id"`
		ChannelID string `json:"channel_id"`
		SubjectID string `json:"subject_id"`
		Phone     string `json:"phone"`
	}{c.tenantID, c.channelID, subjectID, phone})
	if err != nil {
		return Principal{}, errors.New("encode application identity request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Principal{}, errors.New("create application identity request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Application-Identity-Key", c.key)
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Principal{}, errors.New("application identity request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Principal{}, fmt.Errorf("application identity rejected subject: status %d", response.StatusCode)
	}
	var result struct {
		Principal    Principal `json:"principal"`
		DiscoveryURL string    `json:"discovery_url"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<10))
	if err := decoder.Decode(&result); err != nil || result.Principal.ApplicationUserID == "" {
		return Principal{}, errors.New("application identity response is invalid")
	}
	result.Principal.DiscoveryURL = result.DiscoveryURL
	return result.Principal, nil
}
