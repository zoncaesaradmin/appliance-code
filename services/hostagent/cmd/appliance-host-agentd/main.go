package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/httpapi"
	"appliance-code/services/hostagent/internal/mdns"
	"appliance-code/services/hostagent/internal/process"
	"appliance-code/services/hostagent/internal/wifiap"
)

const (
	defaultSocketPath = "/run/zon/host-agent/agent.sock"
	defaultLogPath    = "/data/zon/logs/host-agent/host-agentd.log"
	sharedFSGID       = 20000
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

	wifiManager := wifiap.NewManager()
	mdnsManager := mdns.NewManager()
	server := &http.Server{
		Handler: httpapi.NewHandlerWithControllers(
			bridge.Local{Root: "/", Wifi: wifiManager, MDNS: mdnsManager},
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
