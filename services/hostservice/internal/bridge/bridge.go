package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/hostservice/internal/host"
)

const dialTimeout = 5 * time.Second

type Bridge interface {
	Ping(ctx context.Context) error
	Info(ctx context.Context) (host.Info, error)
	Stats(ctx context.Context) (host.Stats, error)
	Health(ctx context.Context) (host.Health, error)
}

type Local struct {
	Root string
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
		client:     &http.Client{Transport: transport},
	}
}

func (c *UnixSocketClient) Ping(ctx context.Context) error {
	return c.do(ctx, "/healthz", nil)
}

func (c *UnixSocketClient) Info(ctx context.Context) (host.Info, error) {
	var info host.Info
	err := c.do(ctx, "/internal/v1/host/info", &info)
	return info, err
}

func (c *UnixSocketClient) Stats(ctx context.Context) (host.Stats, error) {
	var stats host.Stats
	err := c.do(ctx, "/internal/v1/host/stats", &stats)
	return stats, err
}

func (c *UnixSocketClient) Health(ctx context.Context) (host.Health, error) {
	var health host.Health
	err := c.do(ctx, "/internal/v1/host/health", &health)
	return health, err
}

func (c *UnixSocketClient) do(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("bridge: build request %s: %w", path, err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge: request %s via %s: %w", path, c.socketPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bridge: request %s returned status %d", path, resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("bridge: decode %s response: %w", path, err)
	}
	return nil
}
