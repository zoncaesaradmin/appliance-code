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
	"appliance-code/services/hostagent/internal/mdns"
	"appliance-code/services/hostagent/internal/wifiap"
	"appliance-code/services/hostagent/internal/wificlient"
)

const dialTimeout = 5 * time.Second

type Bridge interface {
	Ping(ctx context.Context) error
	Info(ctx context.Context) (host.Info, error)
	Stats(ctx context.Context) (host.Stats, error)
	Health(ctx context.Context) (host.Health, error)
	WifiStatus(ctx context.Context) (wificlient.Status, error)
	WifiEnable(ctx context.Context) (wificlient.Status, error)
	WifiApply(ctx context.Context, req wificlient.ApplyRequest) (wificlient.Status, error)
	WifiScan(ctx context.Context) (wificlient.ScanResult, error)
	WifiAPStatus(ctx context.Context) (wifiap.Status, error)
	WifiAPApply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error)
	MDNSStatus(ctx context.Context) (mdns.Status, error)
	MDNSApply(ctx context.Context, req mdns.ApplyRequest) (mdns.Status, error)
}

type Local struct {
	Root       string
	WifiClient wificlient.Controller
	WifiAP     wifiap.Controller
	MDNS       mdns.Controller
}

func (l Local) wifiClient() wificlient.Controller {
	if l.WifiClient != nil {
		return l.WifiClient
	}
	manager := wificlient.NewManager()
	return manager
}

func (l Local) wifiAP() wifiap.Controller {
	if l.WifiAP != nil {
		return l.WifiAP
	}
	return wifiap.NewManager()
}

func (l Local) mdnsCtrl() mdns.Controller {
	if l.MDNS != nil {
		return l.MDNS
	}
	return mdns.NewManager()
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

func (l Local) WifiStatus(ctx context.Context) (wificlient.Status, error) {
	return l.wifiClient().Status(ctx)
}

func (l Local) WifiEnable(ctx context.Context) (wificlient.Status, error) {
	return l.wifiClient().Enable(ctx)
}

func (l Local) WifiApply(ctx context.Context, req wificlient.ApplyRequest) (wificlient.Status, error) {
	return l.wifiClient().Apply(ctx, req)
}

func (l Local) WifiScan(ctx context.Context) (wificlient.ScanResult, error) {
	return l.wifiClient().Scan(ctx)
}

func (l Local) WifiAPStatus(ctx context.Context) (wifiap.Status, error) {
	return l.wifiAP().Status(ctx)
}

func (l Local) WifiAPApply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	return l.wifiAP().Apply(ctx, req)
}

func (l Local) MDNSStatus(ctx context.Context) (mdns.Status, error) {
	return l.mdnsCtrl().Status(ctx)
}

func (l Local) MDNSApply(ctx context.Context, req mdns.ApplyRequest) (mdns.Status, error) {
	return l.mdnsCtrl().Apply(ctx, req)
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

func (c *UnixSocketClient) WifiStatus(ctx context.Context) (wificlient.Status, error) {
	var status wificlient.Status
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/wifi", nil, &status)
	return status, err
}

func (c *UnixSocketClient) WifiEnable(ctx context.Context) (wificlient.Status, error) {
	var status wificlient.Status
	err := c.do(ctx, http.MethodPut, "/internal/v1/host/wifi/enable", nil, &status)
	return status, err
}

func (c *UnixSocketClient) WifiApply(ctx context.Context, req wificlient.ApplyRequest) (wificlient.Status, error) {
	var status wificlient.Status
	err := c.do(ctx, http.MethodPut, "/internal/v1/host/wifi", req, &status)
	return status, err
}

func (c *UnixSocketClient) WifiScan(ctx context.Context) (wificlient.ScanResult, error) {
	var result wificlient.ScanResult
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/wifi/scan", nil, &result)
	return result, err
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

func (c *UnixSocketClient) MDNSStatus(ctx context.Context) (mdns.Status, error) {
	var status mdns.Status
	err := c.do(ctx, http.MethodGet, "/internal/v1/host/mdns", nil, &status)
	return status, err
}

func (c *UnixSocketClient) MDNSApply(ctx context.Context, req mdns.ApplyRequest) (mdns.Status, error) {
	var status mdns.Status
	err := c.do(ctx, http.MethodPut, "/internal/v1/host/mdns", req, &status)
	return status, err
}

// WifiController adapters so the pod can implement wifiap.Controller.
func (c *UnixSocketClient) Status(ctx context.Context) (wifiap.Status, error) {
	return c.WifiAPStatus(ctx)
}

func (c *UnixSocketClient) Apply(ctx context.Context, req wifiap.ApplyRequest) (wifiap.Status, error) {
	return c.WifiAPApply(ctx, req)
}

// MDNSController adapters for mdns.Controller on the pod.
type MDNSSocketAdapter struct {
	Client *UnixSocketClient
}

func (a MDNSSocketAdapter) Status(ctx context.Context) (mdns.Status, error) {
	return a.Client.MDNSStatus(ctx)
}

func (a MDNSSocketAdapter) Apply(ctx context.Context, req mdns.ApplyRequest) (mdns.Status, error) {
	return a.Client.MDNSApply(ctx, req)
}

type WifiSocketAdapter struct {
	Client *UnixSocketClient
}

func (a WifiSocketAdapter) Status(ctx context.Context) (wificlient.Status, error) {
	return a.Client.WifiStatus(ctx)
}

func (a WifiSocketAdapter) Enable(ctx context.Context) (wificlient.Status, error) {
	return a.Client.WifiEnable(ctx)
}

func (a WifiSocketAdapter) Apply(ctx context.Context, req wificlient.ApplyRequest) (wificlient.Status, error) {
	return a.Client.WifiApply(ctx, req)
}

func (a WifiSocketAdapter) Scan(ctx context.Context) (wificlient.ScanResult, error) {
	return a.Client.WifiScan(ctx)
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
