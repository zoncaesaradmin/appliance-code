package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"appliance-code/services/ordering/internal/httpapi"
	"appliance-code/services/ordering/internal/order"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var buildVersion = "development"

const (
	databaseAttemptTimeout    = 5 * time.Second
	databaseRetryInitialDelay = 1 * time.Second
	databaseRetryMaxDelay     = 30 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	address := getenv("ORDERING_LISTEN_ADDR", ":8080")
	databaseURL := os.Getenv("ORDERING_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "ORDERING_DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open ordering database")
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(2 * time.Minute)
	store, err := order.NewPostgresStore(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ready := func(ctx context.Context) error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		var version int64
		var dirty bool
		if err := db.QueryRowContext(ctx, `SELECT version, false FROM public.ordering_schema_migrations WHERE version = 1`).Scan(&version, &dirty); err != nil {
			return err
		}
		if dirty || version != 1 {
			return errors.New("ordering migration is not ready")
		}
		return nil
	}
	handler, err := httpapi.New(httpapi.Handler{Store: store, Ready: ready, ChannelID: getenv("ORDERING_CHANNEL_ID", "ordering-web")})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot bind HTTP listener")
		os.Exit(1)
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		err := waitForDatabase(ctx, db)
		switch {
		case err == nil:
			fmt.Fprintln(os.Stdout, "{\"level\":\"info\",\"service\":\"ordering\",\"event\":\"database_ready\",\"message\":\"ordering database ready\"}")
		case !errors.Is(err, context.Canceled):
			fmt.Fprintf(os.Stderr, "ordering database initialization stopped: %v\n", err)
		}
	}()
	errs := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()
	fmt.Fprintf(os.Stdout, "{\"level\":\"info\",\"service\":\"ordering\",\"event\":\"service_started\",\"version\":%q,\"address\":%q}\n", buildVersion, address)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			os.Exit(1)
		}
	case err := <-errs:
		if err != nil {
			os.Exit(1)
		}
	}
}

func waitForDatabase(ctx context.Context, db *sql.DB) error {
	return retryUntilReady(
		ctx,
		func(ctx context.Context) error {
			attemptCtx, cancel := context.WithTimeout(ctx, databaseAttemptTimeout)
			defer cancel()
			if err := db.PingContext(attemptCtx); err != nil {
				return err
			}
			return order.EnsureSchema(attemptCtx, db)
		},
		sleepWithContext,
		func(err error, retryIn time.Duration) {
			fmt.Fprintf(os.Stderr, "{\"level\":\"warn\",\"service\":\"ordering\",\"event\":\"database_unavailable\",\"retry_in\":%q,\"error\":%q}\n", retryIn.String(), err.Error())
		},
	)
}

func retryUntilReady(
	ctx context.Context,
	attempt func(context.Context) error,
	sleep func(context.Context, time.Duration) error,
	onRetry func(error, time.Duration),
) error {
	delay := databaseRetryInitialDelay
	for {
		err := attempt(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if onRetry != nil {
			onRetry(err, delay)
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		delay = nextRetryDelay(delay)
	}
}

func nextRetryDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > databaseRetryMaxDelay {
		return databaseRetryMaxDelay
	}
	return next
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
