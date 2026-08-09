package automationruntimeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/automationruntimeauth"
	"appliance-code/services/controlplane/internal/metadatabundle"
)

type Client struct {
	baseURL string
	http    *http.Client
	token   string
}

func New(baseURL string, hc *http.Client, token string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("automationruntimeclient: base URL is required")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("automationruntimeclient: invalid base URL: %w", err)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    hc,
		token:   token,
	}, nil
}

func (c *Client) Status(ctx context.Context) (metadatabundle.Status, error) {
	var resp struct {
		Status metadatabundle.Status `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/internal/v1/metadata-bundle", nil, &resp); err != nil {
		return metadatabundle.Status{}, err
	}
	return resp.Status, nil
}

func (c *Client) ActiveBundle(ctx context.Context) (*metadatabundle.Bundle, error) {
	var resp struct {
		Status metadatabundle.Status  `json:"status"`
		Bundle *metadatabundle.Bundle `json:"bundle"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/internal/v1/metadata-bundle", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Bundle, nil
}

func (c *Client) ValidateArchive(ctx context.Context, archivePath, signature string) (metadatabundle.ValidationResult, *metadatabundle.Bundle, error) {
	var resp struct {
		Validation metadatabundle.ValidationResult `json:"validation"`
		Bundle     *metadatabundle.Bundle          `json:"bundle"`
	}
	if err := c.doMultipartArchive(ctx, "/internal/v1/metadata-bundle/validate", archivePath, signature, audit.Actor{}, &resp); err != nil {
		return metadatabundle.ValidationResult{}, nil, err
	}
	return resp.Validation, resp.Bundle, nil
}

func (c *Client) InstallArchive(ctx context.Context, actor audit.Actor, archivePath, signature string) (metadatabundle.Status, metadatabundle.ValidationResult, error) {
	var resp struct {
		Status     metadatabundle.Status           `json:"status"`
		Validation metadatabundle.ValidationResult `json:"validation"`
	}
	if err := c.doMultipartArchive(ctx, "/internal/v1/metadata-bundle/install", archivePath, signature, actor, &resp); err != nil {
		return metadatabundle.Status{}, metadatabundle.ValidationResult{}, err
	}
	return resp.Status, resp.Validation, nil
}

func (c *Client) Rollback(ctx context.Context, actor audit.Actor) (metadatabundle.Status, error) {
	var resp metadatabundle.Status
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/metadata-bundle/rollback", map[string]any{"actor": actor}, &resp); err != nil {
		return metadatabundle.Status{}, err
	}
	return resp, nil
}

func (c *Client) InvokeAutomation(ctx context.Context, actor audit.Actor, automationID string, input []byte) (metadatabundle.AutomationInvokeResult, error) {
	if len(input) == 0 {
		input = []byte(`{}`)
	}
	req := map[string]any{
		"actor": actor,
		"input": json.RawMessage(input),
	}
	var resp metadatabundle.AutomationInvokeResult
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/automations/"+url.PathEscape(automationID)+"/invoke", req, &resp); err != nil {
		return metadatabundle.AutomationInvokeResult{}, err
	}
	return resp, nil
}

func (c *Client) doMultipartArchive(ctx context.Context, endpoint, archivePath, signature string, actor audit.Actor, out any) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("archive", path.Base(archivePath))
	if err != nil {
		return err
	}
	if _, err := file.WriteTo(part); err != nil {
		return err
	}
	_ = writer.WriteField("signature", signature)
	if actor.UserID != "" {
		_ = writer.WriteField("actorUserId", actor.UserID)
	}
	if actor.AuthMethod != "" {
		_ = writer.WriteField("actorAuthMethod", actor.AuthMethod)
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeProblem(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, in any, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, &body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeProblem(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) setAuth(req *http.Request) {
	if strings.TrimSpace(c.token) != "" {
		req.Header.Set(automationruntimeauth.HeaderName, c.token)
	}
}

func decodeProblem(resp *http.Response) error {
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		if detail, ok := body["detail"].(string); ok && detail != "" {
			return fmt.Errorf("automationruntimeclient: %s", detail)
		}
		if msg, ok := body["message"].(string); ok && msg != "" {
			return fmt.Errorf("automationruntimeclient: %s", msg)
		}
	}
	return fmt.Errorf("automationruntimeclient: request failed with status %d", resp.StatusCode)
}
