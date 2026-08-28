package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/firewall"
	"appliance-code/services/hostagent/internal/httpapi"
	"appliance-code/services/hostagent/internal/mdns"
	"appliance-code/services/hostagent/internal/process"
	"appliance-code/services/hostagent/internal/wifiap"
	"appliance-code/services/hostagent/internal/wificlient"
)

const (
	defaultSocketPath = "/run/zon/host-agent/agent.sock"
	defaultLogPath    = "/data/zon/logs/host-agent/host-agentd.log"
	sharedFSGID       = 20000

	// Boot radios can take a few seconds to enumerate on mini PCs / USB NICs.
	reconcileAttempts = 10
	reconcileDelay    = 2 * time.Second
)

func main() {
	var socketPath string
	var logPath string
	flag.StringVar(&socketPath, "socket-path", defaultSocketPath, "unix socket path for the host agent daemon")
	flag.StringVar(&logPath, "log-path", defaultLogPath, "host log file path")
	flag.Parse()

	logger := process.NewLogger(logPath)
	if err := prepareSocketPath(socketPath); err != nil {
		logger.Error("prepare socket path failed", "error", err, "socketPath", socketPath)
		os.Exit(1)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		logger.Error("listen failed", "error", err, "socketPath", socketPath)
		os.Exit(1)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		logger.Error("chmod socket failed", "error", err, "socketPath", socketPath)
		os.Exit(1)
	}
	if err := os.Chown(socketPath, 0, sharedFSGID); err != nil {
		logger.Error("chown socket failed", "error", err, "socketPath", socketPath)
		os.Exit(1)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	wifiClientManager := wificlient.NewManager()
	wifiManager := wifiap.NewManager()
	mdnsManager := mdns.NewManager()
	firewallManager := firewall.NewManager()
	go reconcileDay2Features(logger, wifiClientManager, wifiManager, mdnsManager)
	go reconcileApplicationFirewall(logger, firewallManager)

	server := &http.Server{
		Handler: httpapi.NewUnixSocketHandler(
			bridge.Local{Root: "/", WifiClient: wifiClientManager, WifiAP: wifiManager, MDNS: mdnsManager, Firewall: firewallManager},
			wifiClientManager,
			wifiManager,
			mdnsManager,
		),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-process.ShutdownSignal()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	logger.Info("starting appliance host agent daemon", "socketPath", socketPath)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		logger.Error("host agent daemon failed", "error", err)
		os.Exit(1)
	}
}

func reconcileApplicationFirewall(logger *slog.Logger, manager *firewall.Manager) {
	if err := manager.Reconcile(context.Background()); err != nil {
		logger.Warn("application firewall reconcile failed", "error", err)
	}
}

// reconcileDay2Features re-applies persisted day-2 host features after reboot.
// hostapd/wpa_supplicant/dnsmasq are process-backed (not systemd units), so
// desired=true alone is not enough across a restart.
func reconcileDay2Features(logger *slog.Logger, wifiClient *wificlient.Manager, wifiAP *wifiap.Manager, mdnsCtrl *mdns.Manager) {
	ctx := context.Background()
	for attempt := 1; attempt <= reconcileAttempts; attempt++ {
		pending := false

		mdnsStatus, err := mdnsCtrl.Reconcile(ctx)
		if err != nil {
			logger.Warn("mdns reconcile failed", "attempt", attempt, "error", err)
			pending = true
		} else if mdnsStatus.Desired {
			logger.Info("mdns reconcile", "attempt", attempt, "actual", mdnsStatus.Actual, "reason", mdnsStatus.Reason)
			if mdnsStatus.Actual != mdns.ActualActive && mdnsStatus.Reason != mdns.ReasonPackagesMissing {
				pending = true
			}
		}

		apStatus, apErr := wifiAP.Status(ctx)
		clientStatus, clientErr := wifiClient.Status(ctx)
		if apErr != nil {
			logger.Warn("wifi-ap status before reconcile failed", "attempt", attempt, "error", apErr)
			pending = true
		}
		if clientErr != nil {
			logger.Warn("wifi-client status before reconcile failed", "attempt", attempt, "error", clientErr)
			pending = true
		}

		// Mutual exclusion: prefer AP when both are somehow desired.
		switch {
		case apErr == nil && apStatus.Desired:
			status, err := wifiAP.Reconcile(ctx)
			if err != nil {
				logger.Warn("wifi-ap reconcile failed", "attempt", attempt, "error", err)
				pending = true
			} else {
				logger.Info("wifi-ap reconcile", "attempt", attempt, "actual", status.Actual, "reason", status.Reason, "ssid", status.SSID)
				if status.Actual != wifiap.ActualActive && !wifiAPPermanentFailure(status.Reason) {
					pending = true
				}
			}
		case clientErr == nil && clientStatus.Desired:
			status, err := wifiClient.Reconcile(ctx)
			if err != nil {
				logger.Warn("wifi-client reconcile failed", "attempt", attempt, "error", err)
				pending = true
			} else {
				logger.Info("wifi-client reconcile", "attempt", attempt, "actual", status.Actual, "reason", status.Reason, "ssid", status.SSID)
				if status.Actual != wificlient.ActualActive && status.Actual != wificlient.ActualConnecting &&
					status.Reason != wificlient.ReasonPackagesMissing {
					pending = true
				}
			}
		}

		if !pending {
			return
		}
		if attempt < reconcileAttempts {
			time.Sleep(reconcileDelay)
		}
	}
}

func wifiAPPermanentFailure(reason string) bool {
	switch reason {
	case wifiap.ReasonPackagesMissing, wifiap.ReasonPSKMissing:
		return true
	default:
		return false
	}
}

func prepareSocketPath(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o770); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(socketPath), 0o770); err != nil {
		return err
	}
	if err := os.Chown(filepath.Dir(socketPath), 0, sharedFSGID); err != nil {
		return err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
