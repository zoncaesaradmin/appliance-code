package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"appliance-code/sdk/golang/messaging"
	"appliance-code/sdk/golang/servicehealth"
	"appliance-code/services/channel-integration/internal/config"
	"appliance-code/services/channel-integration/internal/identityclient"
	"appliance-code/services/channel-integration/internal/meta"
	"appliance-code/services/channel-integration/internal/otpdelivery"
	"appliance-code/services/channel-integration/internal/webhook"
	"github.com/zoncaesaradmin/platformkit/logging"
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
	metaClient := meta.NewClient(c.GraphAccessToken, c.PhoneNumberID, c.GraphAPIVersion)
	handler := webhook.NewHandler(c.VerifyToken, c.AppSecret, metaClient, logger)
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
	if c.MessagingURL != "" {
		go runOTPDelivery(ctx, c, metaClient, logger)
	}
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

func runOTPDelivery(ctx context.Context, c config.Config, sender *meta.Client, logger logging.Logger) {
	policy := servicehealth.RetryPolicy{InitialDelay: time.Second, MaxDelay: 30 * time.Second, AttemptTimeout: 5 * time.Second}
	for ctx.Err() == nil {
		var client *messaging.Client
		err := policy.Wait(ctx, func(attemptCtx context.Context) error {
			candidate, err := messaging.Connect(messaging.Options{URL: c.MessagingURL, Name: "channel-integration", JetStream: true, Timeout: 5 * time.Second})
			if err != nil {
				return err
			}
			if err := candidate.EnsureStream(attemptCtx, messaging.StreamSpec{Name: "identity", Subjects: []string{"event.identity.>"}, MaxAge: 24 * time.Hour}); err != nil {
				candidate.Close()
				return err
			}
			client = candidate
			return nil
		}, func(_ error, retryIn time.Duration) {
			logger.Warnw("OTP delivery messaging unavailable", "event", "otp_delivery_dependency_unavailable", "retry_in", retryIn.String(), "error_kind", "messaging_unavailable")
		})
		if err != nil {
			return
		}
		err = otpdelivery.Run(ctx, client, c.OTPDeliveryKey, c.OTPTemplateName, c.OTPTemplateLanguage, sender, logger)
		client.Close()
		if ctx.Err() != nil {
			return
		}
		errorKind := "consumer_stopped"
		if err != nil {
			errorKind = "consumer_error"
		}
		logger.Warnw("OTP delivery consumer stopped; reconnecting", "event", "otp_delivery_consumer_stopped", "error_kind", errorKind)
	}
}
