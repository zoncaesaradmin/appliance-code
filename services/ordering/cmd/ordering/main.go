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

func main() {
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
	if err := order.EnsureSchema(context.Background(), db); err != nil {
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

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
