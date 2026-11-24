package main

import (
	"appliance-code/services/channel-integration/internal/config"
	"appliance-code/services/channel-integration/internal/identityclient"
	"appliance-code/services/channel-integration/internal/meta"
	"appliance-code/services/channel-integration/internal/webhook"
	"context"
	"errors"
	"fmt"
	"github.com/zoncaesaradmin/platformkit/logging"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var buildVersion = "development"

func main() {
	logger, err := logging.NewLogger(&logging.LoggerConfig{Level: logging.InfoLevel, OutputTarget: logging.OutputTargetStdout, LoggerName: "channel-integration", ServiceName: "channel-integration"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot initialize logger")
		os.Exit(1)
	}
	defer logger.Close()
	c, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	mux := http.NewServeMux()
	handler := webhook.NewHandler(c.VerifyToken, c.AppSecret, meta.NewClient(c.GraphAccessToken, c.PhoneNumberID, c.GraphAPIVersion), logger)
	if c.IdentityURL != "" {
		resolver, err := identityclient.New(c.IdentityURL, c.IdentityKey, c.IdentityTenantID, c.IdentityChannelID)
		if err != nil {
			logger.Error("cannot initialize application identity resolver")
			os.Exit(1)
		}
		handler.SetPrincipalResolver(resolver)
	}
	handler.Register(mux)
	server := &http.Server{Addr: c.ListenAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		logger.Error("cannot bind HTTP listener")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()
	logger.Infow("channel integration started", "event", "service_started", "address", c.ListenAddress, "version", buildVersion)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP shutdown failed")
			os.Exit(1)
		}
	case err := <-errs:
		if err != nil {
			logger.Error("HTTP server stopped unexpectedly")
			os.Exit(1)
		}
	}
}
