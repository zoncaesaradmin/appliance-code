package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/config"
	"appliance-code/services/hostagent/internal/httpapi"
	"appliance-code/services/hostagent/internal/process"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	logger := process.NewLogger(cfg.ApplicationLogPath)
	client := bridge.NewUnixSocketClient(cfg.SocketPath)
	// Pod forwards host facts, wifi-ap, and mdns control over the host-agentd socket.
	handler := httpapi.NewHandlerWithControllers(client, client, bridge.MDNSSocketAdapter{Client: client})
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-process.ShutdownSignal()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	logger.Info("starting appliance host agent", "addr", cfg.Addr, "socketPath", cfg.SocketPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("host agent failed", "error", err)
		os.Exit(1)
	}
}
