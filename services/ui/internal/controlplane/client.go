package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	uilogging "appliance-code/services/ui/internal/logging"
)

type Config struct {
	BaseURL         string
	InternalBaseURL string
	HTTPClient      *http.Client
	Logger          uilogging.Logger
	TraceHTTP       bool
}

type Client struct {
	baseURL         string
	internalBaseURL string
	httpClient      *http.Client
	logger          uilogging.Logger
	traceHTTP       bool
}

type LoginResult struct {
	AccessToken     string    `json:"accessToken"`
	RefreshToken    string    `json:"refreshToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

type Session struct {
	UserID      string   `json:"userId"`
	Username    string   `json:"username"`
	AuthMethod  string   `json:"authMethod"`
	Permissions []string `json:"permissions"`
}

type Version struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	GoVersion string `json:"goVersion"`
}

type Health struct {
	Status string `json:"status"`
}

var ErrAlreadyInitialized = errors.New("controlplane: appliance is already initialized")

type HTTPStatusError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s %s: got HTTP %d", e.Method, e.Path, e.StatusCode)
}

type SetupStatus struct {
	Initialized bool `json:"initialized"`
}

type WorkProfile struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Repos       []WorkProfileRepo `json:"repos,omitempty"`
}

type WorkProfileRepo struct {
	Name             string `json:"name"`
	EnabledByDefault bool   `json:"enabledByDefault,omitempty"`
}

type Workspace struct {
	ID            string     `json:"id"`
	OwnerID       string     `json:"ownerId"`
	Name          string     `json:"name"`
	WorkProfile   string     `json:"workProfile"`
	SourceRepoURL string     `json:"sourceRepoUrl"`
	SourceRef     string     `json:"sourceRef"`
	Status        string     `json:"status"`
	ReasonCode    string     `json:"reasonCode,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
}

type CreateWorkspaceRequest struct {
	Name        string `json:"name"`
	WorkProfile string `json:"workProfile"`
}

func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		internalBaseURL: strings.TrimRight(cfg.InternalBaseURL, "/"),
		httpClient:      httpClient,
		logger:          cfg.Logger,
		traceHTTP:       cfg.TraceHTTP,
	}
}

func (c *Client) Login(ctx context.Context, username, password string) (LoginResult, error) {
	var out LoginResult
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (LoginResult, error) {
	var out LoginResult
	body, _ := json.Marshal(map[string]string{"refreshToken": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) Logout(ctx context.Context, accessToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/logout", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return c.doJSON(req, http.StatusNoContent, nil)
}

func (c *Client) Session(ctx context.Context, accessToken string) (Session, error) {
	var out Session
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/auth/session", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var out Version
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.internalBaseURL+"/version", nil)
	if err != nil {
		return out, err
	}
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) SetupStatus(ctx context.Context) (SetupStatus, error) {
	var out SetupStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/setup/status", nil)
	if err != nil {
		return out, err
	}
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) CreateFirstAdmin(ctx context.Context, username, password, displayName string) error {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password, "displayName": displayName})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/setup/first-admin", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.doJSON(req, http.StatusCreated, nil); err == nil {
		return nil
	} else {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusConflict {
			return ErrAlreadyInitialized
		}
		return err
	}
}

func (c *Client) Ready(ctx context.Context) (Health, error) {
	var out Health
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.internalBaseURL+"/health/ready", nil)
	if err != nil {
		return out, err
	}
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ListWorkProfiles(ctx context.Context, accessToken string) ([]WorkProfile, error) {
	var result struct {
		Items []WorkProfile `json:"items"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/work-profiles", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := c.doJSON(req, http.StatusOK, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) ListWorkspaces(ctx context.Context, accessToken string) ([]Workspace, error) {
	var result struct {
		Items []Workspace `json:"items"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/workspaces", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := c.doJSON(req, http.StatusOK, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *Client) CurrentWorkspace(ctx context.Context, accessToken string) (Workspace, error) {
	var out Workspace
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/current-workspace", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) CreateWorkspace(ctx context.Context, accessToken string, in CreateWorkspaceRequest) (Workspace, error) {
	var out Workspace
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/workspaces", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if err := c.doJSON(req, http.StatusCreated, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) SetCurrentWorkspace(ctx context.Context, accessToken, workspaceID string) (Workspace, error) {
	var out Workspace
	body, _ := json.Marshal(map[string]string{"workspaceId": workspaceID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/current-workspace", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if err := c.doJSON(req, http.StatusOK, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) DeleteWorkspace(ctx context.Context, accessToken, workspaceID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/v1/workspaces/"+workspaceID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return c.doJSON(req, http.StatusNoContent, nil)
}

func (c *Client) doJSON(req *http.Request, wantStatus int, out any) error {
	body, err := c.do(req, wantStatus)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (c *Client) do(req *http.Request, wantStatus int) ([]byte, error) {
	requestBody := cloneRequestBody(req)
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.trace(req, wantStatus, 0, time.Since(start), requestBody, nil, err)
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.trace(req, wantStatus, resp.StatusCode, time.Since(start), requestBody, nil, readErr)
		return nil, readErr
	}
	if resp.StatusCode != wantStatus {
		statusErr := &HTTPStatusError{
			Method:     req.Method,
			Path:       req.URL.Path,
			StatusCode: resp.StatusCode,
			Body:       string(limitBytes(responseBody, 4096)),
		}
		c.trace(req, wantStatus, resp.StatusCode, time.Since(start), requestBody, responseBody, statusErr)
		return nil, statusErr
	}

	c.trace(req, wantStatus, resp.StatusCode, time.Since(start), requestBody, responseBody, nil)
	return responseBody, nil
}

func (c *Client) trace(req *http.Request, wantStatus, status int, duration time.Duration, requestBody, responseBody []byte, callErr error) {
	if !c.traceHTTP || c.logger == nil {
		return
	}

	args := []any{
		"component", "ui-controlplane-client",
		"method", req.Method,
		"path", req.URL.Path,
		"wantStatus", wantStatus,
		"status", status,
		"duration", duration.String(),
	}
	if requestSummary := summarizeJSONForLog(requestBody); requestSummary != nil {
		args = append(args, "request", requestSummary)
	}
	if responseSummary := summarizeJSONForLog(responseBody); responseSummary != nil {
		args = append(args, "response", responseSummary)
	}
	if callErr != nil {
		args = append(args, "error", callErr.Error())
		c.logger.Warnw("control plane API call", args...)
		return
	}
	c.logger.Infow("control plane API call", args...)
}

func cloneRequestBody(req *http.Request) []byte {
	if req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		req.Body = io.NopCloser(bytes.NewReader(nil))
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func summarizeJSONForLog(body []byte) any {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err == nil {
		return redactJSONValue(decoded)
	}

	limited := limitBytes(body, 1024)
	return map[string]any{
		"raw":       string(limited),
		"truncated": len(limited) < len(body),
	}
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = redactJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactJSONValue(child)
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range []string{"password", "token", "authorization", "secret", "privatekey", "private_key", "credential"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func limitBytes(body []byte, max int) []byte {
	if len(body) <= max {
		return body
	}
	return body[:max]
}
