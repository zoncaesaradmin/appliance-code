package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"appliance-code/sdk/golang/applianceclient"
)

type config struct {
	apiBaseURL    string
	profile       string
	adminUsername string
	adminPassword string
}

type runner struct {
	cfg    config
	client *applianceclient.Client
	http   *http.Client
	logger *log.Logger
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.apiBaseURL, "api-base-url", "", "control-plane API base URL (required)")
	flag.StringVar(&cfg.profile, "profile", "", "appliance profile under test (required)")
	flag.StringVar(&cfg.adminUsername, "admin-username", "admin", "bootstrap administrator username")
	flag.StringVar(&cfg.adminPassword, "admin-password", "", "bootstrap administrator password (required)")
	flag.Parse()

	switch {
	case cfg.apiBaseURL == "":
		fatalf("missing --api-base-url")
	case cfg.profile == "":
		fatalf("missing --profile")
	case cfg.adminPassword == "":
		fatalf("missing --admin-password")
	}

	r := &runner{
		cfg:    cfg,
		client: applianceclient.New(cfg.apiBaseURL),
		http:   &http.Client{Timeout: 15 * time.Second},
		logger: log.New(os.Stdout, "profile-gating: ", log.LstdFlags|log.Lmicroseconds),
	}
	if err := r.run(context.Background()); err != nil {
		r.logger.Printf("FAILED: %v", err)
		os.Exit(1)
	}
	r.logger.Printf("PASSED profile=%s", cfg.profile)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

func (r *runner) run(ctx context.Context) error {
	r.logger.Printf("logging in as admin for profile=%s", r.cfg.profile)
	login, err := r.client.Login(ctx, r.cfg.adminUsername, r.cfg.adminPassword)
	if err != nil {
		return fmt.Errorf("admin login: %w", err)
	}
	accessToken := login.AccessToken

	for _, path := range []string{
		"/api/v1/work-profiles",
		"/api/v1/current-workspace",
		"/api/v1/current-workspace/build-targets",
		"/api/v1/jobs",
		"/api/v1/builds",
	} {
		if err := r.expectStatus(ctx, accessToken, http.MethodGet, path, http.StatusNotFound); err != nil {
			return err
		}
	}

	sessionID, err := r.initializeMCP(ctx, accessToken)
	if err != nil {
		return err
	}
	toolsResp, err := r.mcpPOST(ctx, accessToken, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      "tools-1",
		"method":  "tools/list",
	})
	if err != nil {
		return err
	}
	toolNames, err := mcpToolNames(toolsResp)
	if err != nil {
		return err
	}
	for _, unexpected := range []string{"list_work_profiles", "submit_build", "get_job_status"} {
		if toolNames[unexpected] {
			return fmt.Errorf("profile %s unexpectedly exposed MCP build tool %q", r.cfg.profile, unexpected)
		}
	}

	callResp, err := r.mcpPOST(ctx, accessToken, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      "submit-1",
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "submit_build",
			"arguments": map[string]any{"targetName": "app"},
		},
	})
	if err != nil {
		return err
	}
	if err := expectRPCError(callResp, -32601, "Tool not found"); err != nil {
		return fmt.Errorf("direct disabled submit_build call: %w", err)
	}
	return nil
}

func (r *runner) expectStatus(ctx context.Context, bearer, method, path string, want int) error {
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s status = %d, want %d body=%s", method, path, resp.StatusCode, want, string(body))
	}
	return nil
}

func (r *runner) initializeMCP(ctx context.Context, bearer string) (string, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      "init-1",
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"clientInfo": map[string]any{
				"name":    "appliance-profile-gating-e2e",
				"version": "1.0.0",
			},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.apiBaseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+bearer)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("POST /mcp initialize status = %d body=%s", resp.StatusCode, string(responseBody))
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return "", fmt.Errorf("initialize response missing Mcp-Session-Id header")
	}
	return sessionID, nil
}

func (r *runner) mcpPOST(ctx context.Context, bearer, sessionID string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.apiBaseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /mcp status = %d body=%s", resp.StatusCode, string(responseBody))
	}
	var decoded map[string]any
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func mcpToolNames(resp map[string]any) (map[string]bool, error) {
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tools/list missing result: %#v", resp)
	}
	items, ok := result["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("tools/list result missing tools: %#v", resp)
	}
	names := make(map[string]bool, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool has type %T, want object", item)
		}
		name, ok := tool["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("tool missing non-empty name: %#v", tool)
		}
		names[name] = true
	}
	return names, nil
}

func expectRPCError(resp map[string]any, wantCode int, wantMessage string) error {
	errValue, ok := resp["error"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing JSON-RPC error: %#v", resp)
	}
	code, ok := errValue["code"].(float64)
	if !ok || int(code) != wantCode {
		return fmt.Errorf("error code = %v, want %d in %#v", errValue["code"], wantCode, resp)
	}
	message, ok := errValue["message"].(string)
	if !ok || message != wantMessage {
		return fmt.Errorf("error message = %v, want %q in %#v", errValue["message"], wantMessage, resp)
	}
	return nil
}
