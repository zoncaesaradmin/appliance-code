package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/hostagent/internal/host"
	"appliance-code/services/hostagent/internal/wifiap"
)

const dialTimeout = 5 * time.Second

type Bridge interface {
	Ping(ctx context.Context) error
	Info(ctx context.Context) (host.Info, error)
	Stats(ctx context.Context) (host.Stats, error)
	Health(ctx context.Context) (host.Health, error)
	WifiAPStatus(ctx context.Context) (wifiap.Status, error)
	WifiAPApply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error)
}

type Local struct {
	Root string
	Wifi wifiap.Controller
}

func (l Local) wifi() wifiap.Controller {
	if l.Wifi != nil {
		return l.Wifi
	}
	return wifiap.NewManager()
}

func (l Local) Ping(context.Context) error {
	return nil
}

func (l Local) Info(context.Context) (host.Info, error) {
	return host.CollectInfo(l.Root)
}

func (l Local) Stats(context.Context) (host.Stats, error) {
	return host.CollectStats(l.Root)
}

func (l Local) Health(context.Context) (host.Health, error) {
	return host.CollectHealth(l.Root), nil
}

func (l Local) WifiAPStatus(ctx context.Context) (wifiap.Status, error) {
	return l.wifi().Status(ctx)
}

func (l Local) WifiAPApply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	return l.wifi().Apply(ctx, req)
}

type UnixSocketClient struct {
	socketPath string
	baseURL    string
	client     *http.Client
}

func NewUnixSocketClient(socketPath string) *UnixSocketClient {
	socketPath = strings.TrimSpace(socketPath)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: dialTimeout}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &UnixSocketClient{
		socketPath: socketPath,
		baseURL:    "http://host-agentd",
		client:     &http.Client{Transport: transport, Timeout: 60 * time.Second},
	}
}

func (c *UnixSocketClient) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil)
}

func (c *UnixSocketClient) Info(ctx context.Context) (host.Info, error) {
	var info host.Info
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/info", nil, &info)
	return info, err
}

func (c *UnixSocketClient) Stats(ctx context.Context) (host.Stats, error) {
	var stats host.Stats
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/stats", nil, &stats)
	return stats, err
}

func (c *UnixSocketClient) Health(ctx context.Context) (host.Health, error) {
	var health host.Health
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/health", nil, &health)
	return health, err
}

func (c *UnixSocketClient) WifiAPStatus(ctx context.Context) (wifiap.Status, error) {
	var status wifiap.Status
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/wifi-ap", nil, &status)
	return status, err
}

func (c *UnixSocketClient) WifiAPApply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	var status wifiap.Status
	err := c.do(ctx, http.MethodPut, "/internal/v1/host/wifi-ap", req, &status)
	return status, err
}

// WifiController adapts the Unix socket client to wifiap.Controller so the
// in-cluster host-agent pod can implement the same HTTP routes as the daemon.
func (c *UnixSocketClient) Status(ctx context.Context) (wifiap.Status, error) {
	return c.WifiAPStatus(ctx)
}

func (c *UnixSocketClient) Apply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	return c.WifiAPApply(ctx, req)
}

func (c *UnixSocketClient) do(ctx context.Context, method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("bridge: encode %s body: %w", path, err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("bridge: build request %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge: request %s via %s: %w", path, c.socketPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("bridge: request %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("bridge: decode %s response: %w", path, err)
	}
	return nil
}
