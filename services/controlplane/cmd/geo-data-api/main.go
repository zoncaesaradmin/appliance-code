package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"appliance-code/services/controlplane/internal/geodata"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	closeLog, err := configureLog()
	if err != nil {
		fmt.Fprintln(os.Stderr, "geo-data-api:", err)
		os.Exit(1)
	}
	defer closeLog()
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func configureLog() (func(), error) {
	path := strings.TrimSpace(os.Getenv("GEO_LOG_PATH"))
	if path == "" {
		log.SetOutput(os.Stderr)
		return func() {}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	return func() { _ = file.Close() }, nil
}

func secretFile(env string) (string, error) {
	path := strings.TrimSpace(os.Getenv(env))
	if path == "" {
		return "", fmt.Errorf("%s is required", env)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", env, err)
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("%s is empty", env)
	}
	return value, nil
}

func run() error {
	databaseURL, err := secretFile("GEO_DATABASE_URL_FILE")
	if err != nil {
		return err
	}
	token, err := secretFile("GEO_INTERNAL_TOKEN_FILE")
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(2 * time.Minute)
	handler, err := geodata.NewHandler(geodata.PostgresStore{DB: db}, token)
	if err != nil {
		return err
	}
	addr := strings.TrimSpace(os.Getenv("GEO_LISTEN_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	log.Printf("listening on %s", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve geo API: %w", err)
	}
}
