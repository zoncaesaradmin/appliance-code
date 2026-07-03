// Package applianceclient is a Go client SDK for the appliance control
// plane's REST API: interactive login/refresh/logout, API token lifecycle,
// and the OCI registry token endpoint. It is a plain HTTP client with no
// dependency on the server's implementation; it never imports anything
// under appliance-code/server/backend/internal.
package applianceclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls one appliance control plane's REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client constructed by New.
type Option func(*Client)

// WithHTTPClient overrides the default *http.Client, e.g. to configure TLS
// trust or a custom timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New builds a Client for the appliance at baseURL (e.g.
// "https://appliance.example.internal").
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// credential is a request's authentication, at most one of which applies.
type credential struct {
	bearer        string
	basicUser     string
	basicPassword string
}

func bearerCredential(token string) credential { return credential{bearer: token} }
func basicCredential(user, password string) credential {
	return credential{basicUser: user, basicPassword: password}
}

func (c *Client) do(ctx context.Context, method, path string, cred credential, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("applianceclient: encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("applianceclient: building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cred.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+cred.bearer)
	}
	if cred.basicUser != "" {
		req.SetBasicAuth(cred.basicUser, cred.basicPassword)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("applianceclient: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var problem Problem
		if decodeErr := json.NewDecoder(resp.Body).Decode(&problem); decodeErr == nil && problem.Title != "" {
			return &problem
		}
		return fmt.Errorf("applianceclient: %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("applianceclient: decoding response from %s %s: %w", method, path, err)
	}
	return nil
}

// Login authenticates with a username and password and returns a new
// interactive session's access and refresh tokens.
func (c *Client) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	var result LoginResult
	body := map[string]string{"username": username, "password": password}
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", credential{}, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Refresh rotates a session's refresh token and returns a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*LoginResult, error) {
	var result LoginResult
	body := map[string]string{"refreshToken": refreshToken}
	if err := c.do(ctx, http.MethodPost, "/api/v1/auth/refresh", credential{}, body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Logout revokes the session family behind accessToken.
func (c *Client) Logout(ctx context.Context, accessToken string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/auth/logout", bearerCredential(accessToken), nil, nil)
}

// Session returns the authenticated principal behind accessToken.
func (c *Client) Session(ctx context.Context, accessToken string) (*SessionInfo, error) {
	var result SessionInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/session", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateUserRequest describes a new local user.
type CreateUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

// CreateUser creates a new local user.
func (c *Client) CreateUser(ctx context.Context, accessToken string, req CreateUserRequest) (*User, error) {
	var result User
	if err := c.do(ctx, http.MethodPost, "/api/v1/users", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListUsers lists every user visible to the caller.
func (c *Client) ListUsers(ctx context.Context, accessToken string) ([]User, error) {
	var result struct {
		Items []User `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/users", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// GetUser returns one user by ID.
func (c *Client) GetUser(ctx context.Context, accessToken, userID string) (*User, error) {
	var result User
	if err := c.do(ctx, http.MethodGet, "/api/v1/users/"+url.PathEscape(userID), bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateUserDisplayName updates the only mutable user profile field in v1.
func (c *Client) UpdateUserDisplayName(ctx context.Context, accessToken, userID, displayName string) (*User, error) {
	var result User
	body := map[string]string{"displayName": displayName}
	if err := c.do(ctx, http.MethodPatch, "/api/v1/users/"+url.PathEscape(userID), bearerCredential(accessToken), body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DisableUser disables a user account.
func (c *Client) DisableUser(ctx context.Context, accessToken, userID string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(userID)+"/disable", bearerCredential(accessToken), nil, nil)
}

// EnableUser re-enables a user account.
func (c *Client) EnableUser(ctx context.Context, accessToken, userID string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(userID)+"/enable", bearerCredential(accessToken), nil, nil)
}

// UnlockUser clears a user's durable login lockout.
func (c *Client) UnlockUser(ctx context.Context, accessToken, userID string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(userID)+"/unlock", bearerCredential(accessToken), nil, nil)
}

// InitiatePasswordReset triggers an administrator-issued password reset for userID.
func (c *Client) InitiatePasswordReset(ctx context.Context, accessToken, userID string) (*PasswordResetResult, error) {
	var result PasswordResetResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(userID)+"/password-reset", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetUserRoles replaces the target user's role assignment set.
func (c *Client) SetUserRoles(ctx context.Context, accessToken, userID string, roleIDs []string) error {
	body := map[string][]string{"roleIds": roleIDs}
	return c.do(ctx, http.MethodPut, "/api/v1/users/"+url.PathEscape(userID)+"/roles", bearerCredential(accessToken), body, nil)
}

// ListRoles lists every role.
func (c *Client) ListRoles(ctx context.Context, accessToken string) ([]Role, error) {
	var result struct {
		Items []Role `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/roles", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ListPermissions lists the published permission catalog.
func (c *Client) ListPermissions(ctx context.Context, accessToken string) ([]Permission, error) {
	var result struct {
		Items []Permission `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/permissions", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// CreateRoleRequest describes a new custom role.
type CreateRoleRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// CreateRole creates a custom role.
func (c *Client) CreateRole(ctx context.Context, accessToken string, req CreateRoleRequest) (*Role, error) {
	var result Role
	if err := c.do(ctx, http.MethodPost, "/api/v1/roles", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateRole replaces a custom role's permission set.
func (c *Client) UpdateRole(ctx context.Context, accessToken, roleID string, permissions []string) error {
	body := map[string][]string{"permissions": permissions}
	return c.do(ctx, http.MethodPut, "/api/v1/roles/"+url.PathEscape(roleID), bearerCredential(accessToken), body, nil)
}

// DeleteRole removes a custom role.
func (c *Client) DeleteRole(ctx context.Context, accessToken, roleID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/roles/"+url.PathEscape(roleID), bearerCredential(accessToken), nil, nil)
}

// CreateTokenRequest describes a new API token to create for the caller.
type CreateTokenRequest struct {
	Name            string   `json:"name"`
	LifetimeSeconds int64    `json:"lifetimeSeconds,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
}

// CreateToken issues a new API token for the authenticated caller. The raw
// token value is returned only once, in the result.
func (c *Client) CreateToken(ctx context.Context, accessToken string, req CreateTokenRequest) (*CreateTokenResult, error) {
	var result CreateTokenResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/tokens", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTokens returns the authenticated caller's own API tokens.
func (c *Client) ListTokens(ctx context.Context, accessToken string) ([]APIToken, error) {
	var result struct {
		Items []APIToken `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/tokens", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// RevokeToken revokes an API token the caller owns (or any token, if the
// caller holds tokens.revoke.any).
func (c *Client) RevokeToken(ctx context.Context, accessToken, tokenID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(tokenID), bearerCredential(accessToken), nil, nil)
}

// CreateTokenForUser issues a new API token owned by another user.
func (c *Client) CreateTokenForUser(ctx context.Context, accessToken, userID string, req CreateTokenRequest) (*CreateTokenResult, error) {
	var result CreateTokenResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(userID)+"/tokens", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RevokeTokenForUser revokes a token owned by another user.
func (c *Client) RevokeTokenForUser(ctx context.Context, accessToken, userID, tokenID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/users/"+url.PathEscape(userID)+"/tokens/"+url.PathEscape(tokenID), bearerCredential(accessToken), nil, nil)
}

// RegistryToken requests a short-lived OCI registry access token, following
// the OCI Distribution token-service contract: authentication is HTTP
// Basic with the appliance username and an API token, never a password.
func (c *Client) RegistryToken(ctx context.Context, username, apiToken, service string, scopes []string) (*RegistryTokenResult, error) {
	q := url.Values{}
	q.Set("service", service)
	for _, s := range scopes {
		q.Add("scope", s)
	}

	var result RegistryTokenResult
	path := "/api/v1/registry/token?" + q.Encode()
	if err := c.do(ctx, http.MethodGet, path, basicCredential(username, apiToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateRegistryGrantRequest describes a new repository-prefix grant.
type CreateRegistryGrantRequest struct {
	SubjectType string   `json:"subjectType"`
	SubjectID   string   `json:"subjectId"`
	PathPrefix  string   `json:"pathPrefix"`
	Actions     []string `json:"actions"`
}

// CreateRegistryGrant creates a repository-prefix grant. The caller must
// hold registry.grants.write.
func (c *Client) CreateRegistryGrant(ctx context.Context, accessToken string, req CreateRegistryGrantRequest) (*RegistryGrant, error) {
	var result RegistryGrant
	if err := c.do(ctx, http.MethodPost, "/api/v1/registry/grants", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListRegistryGrants lists every repository-prefix grant. The caller must
// hold registry.grants.read.
func (c *Client) ListRegistryGrants(ctx context.Context, accessToken string) ([]RegistryGrant, error) {
	var result struct {
		Items []RegistryGrant `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/registry/grants", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// DeleteRegistryGrant removes a repository-prefix grant. The caller must
// hold registry.grants.write.
func (c *Client) DeleteRegistryGrant(ctx context.Context, accessToken, grantID string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/registry/grants/"+url.PathEscape(grantID), bearerCredential(accessToken), nil, nil)
}

// ListRegistryRepositories lists the registry repositories visible to the caller.
func (c *Client) ListRegistryRepositories(ctx context.Context, accessToken string) ([]string, error) {
	var result struct {
		Items []string `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/registry/repositories", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ListRegistryTags lists tags for repository.
func (c *Client) ListRegistryTags(ctx context.Context, accessToken, repository string) ([]string, error) {
	var result struct {
		Items []string `json:"items"`
	}
	path := "/api/v1/registry/repositories/" + repositoryPath(repository) + "/tags"
	if err := c.do(ctx, http.MethodGet, path, bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ListRegistryReferrers lists referrers for repository@digest.
func (c *Client) ListRegistryReferrers(ctx context.Context, accessToken, repository, digest string) ([]RegistryReferrer, error) {
	var result struct {
		Items []RegistryReferrer `json:"items"`
	}
	path := "/api/v1/registry/repositories/" + repositoryPath(repository) + "/referrers?digest=" + url.QueryEscape(digest)
	if err := c.do(ctx, http.MethodGet, path, bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// CreateBuildRequest describes a new build.
type CreateBuildRequest struct {
	SourceRepoURL      string `json:"sourceRepoUrl"`
	SourceCommitSHA    string `json:"sourceCommitSha"`
	ContainerfilePath  string `json:"containerfilePath,omitempty"`
	ImageRepository    string `json:"imageRepository"`
	ImageTag           string `json:"imageTag"`
	BuilderImageDigest string `json:"builderImageDigest"`
}

// CreateBuild submits a new build.
func (c *Client) CreateBuild(ctx context.Context, accessToken string, req CreateBuildRequest) (*Build, error) {
	var result Build
	if err := c.do(ctx, http.MethodPost, "/api/v1/builds", bearerCredential(accessToken), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListBuilds lists builds visible to the caller.
func (c *Client) ListBuilds(ctx context.Context, accessToken string) ([]Build, error) {
	var result struct {
		Items []Build `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/builds", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// GetBuild returns one build by ID.
func (c *Client) GetBuild(ctx context.Context, accessToken, buildID string) (*Build, error) {
	var result Build
	if err := c.do(ctx, http.MethodGet, "/api/v1/builds/"+url.PathEscape(buildID), bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelBuild cancels one build.
func (c *Client) CancelBuild(ctx context.Context, accessToken, buildID string) (*Build, error) {
	var result Build
	if err := c.do(ctx, http.MethodPost, "/api/v1/builds/"+url.PathEscape(buildID)+"/cancel", bearerCredential(accessToken), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BuildLogs returns text logs for one build.
func (c *Client) BuildLogs(ctx context.Context, accessToken, buildID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/builds/"+url.PathEscape(buildID)+"/logs", nil)
	if err != nil {
		return "", fmt.Errorf("applianceclient: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("applianceclient: GET /api/v1/builds/%s/logs: %w", buildID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var problem Problem
		if decodeErr := json.NewDecoder(resp.Body).Decode(&problem); decodeErr == nil && problem.Title != "" {
			return "", &problem
		}
		return "", fmt.Errorf("applianceclient: GET /api/v1/builds/%s/logs: unexpected status %d", buildID, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("applianceclient: reading build logs: %w", err)
	}
	return string(body), nil
}

func repositoryPath(repository string) string {
	parts := strings.Split(repository, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
