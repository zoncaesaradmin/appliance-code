package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL         string
	InternalBaseURL string
	HTTPClient      *http.Client
}

type Client struct {
	baseURL         string
	internalBaseURL string
	httpClient      *http.Client
}

type LoginResult struct {
	AccessToken     string    `json:"accessToken"`
	RefreshToken    string    `json:"refreshToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

type Session struct {
	UserID      string   `json:"userId"`
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

type SetupStatus struct {
	Initialized bool `json:"initialized"`
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
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusConflict:
		return ErrAlreadyInitialized
	default:
		return fmt.Errorf("%s %s: got HTTP %d, want %d", req.Method, req.URL.Path, resp.StatusCode, http.StatusCreated)
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

func (c *Client) doJSON(req *http.Request, wantStatus int, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("%s %s: got HTTP %d, want %d", req.Method, req.URL.Path, resp.StatusCode, wantStatus)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
