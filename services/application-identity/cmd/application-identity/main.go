// application-identity exposes application onboarding and domain APIs. Its
// authentication routes are a thin proxy to the configured Auth Service.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"appliance-code/sdk/golang/servicehealth"
	"appliance-code/services/application-identity/internal/identity"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var buildVersion = "development"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS identity_channel_subjects (id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'), channel_id text NOT NULL REFERENCES identity_channels (id), external_subject_key bytea NOT NULL CHECK (octet_length(external_subject_key) = 32), user_id text REFERENCES identity_users (id), status text NOT NULL CHECK (status IN ('pending', 'linked', 'blocked')), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, UNIQUE (channel_id, external_subject_key));
CREATE INDEX IF NOT EXISTS identity_channel_subjects_user_idx ON identity_channel_subjects (user_id);
CREATE TABLE IF NOT EXISTS identity_zones (id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES identity_tenants (id), name text NOT NULL, latitude double precision NOT NULL CHECK (latitude >= -90 AND latitude <= 90), longitude double precision NOT NULL CHECK (longitude >= -180 AND longitude <= 180), join_radius_m integer NOT NULL DEFAULT 1000 CHECK (join_radius_m BETWEEN 1 AND 10000), status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')), created_at timestamptz NOT NULL);
ALTER TABLE identity_zones ADD COLUMN IF NOT EXISTS join_radius_m integer NOT NULL DEFAULT 1000;
DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'identity_zones_join_radius_m_check') THEN ALTER TABLE identity_zones ADD CONSTRAINT identity_zones_join_radius_m_check CHECK (join_radius_m BETWEEN 1 AND 10000); END IF; END $$;
CREATE INDEX IF NOT EXISTS identity_zones_tenant_status_idx ON identity_zones (tenant_id, status);
CREATE TABLE IF NOT EXISTS identity_discovery_links (token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32), user_id text NOT NULL REFERENCES identity_users (id), tenant_id text NOT NULL REFERENCES identity_tenants (id), channel_id text NOT NULL REFERENCES identity_channels (id), audience text NOT NULL, expires_at timestamptz NOT NULL, used_at timestamptz, created_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_discovery_links_expiry_idx ON identity_discovery_links (expires_at);
CREATE TABLE IF NOT EXISTS identity_zone_join_requests (user_id text NOT NULL REFERENCES identity_users (id), tenant_id text NOT NULL REFERENCES identity_tenants (id), zone_id text NOT NULL REFERENCES identity_zones (id), status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')), approval_mode text NOT NULL CHECK (approval_mode IN ('admin_review', 'auto_no_admin')), approved_at timestamptz, approved_by_user_id text REFERENCES identity_users (id), requested_at timestamptz NOT NULL, PRIMARY KEY (user_id, zone_id));
ALTER TABLE identity_zone_join_requests ADD COLUMN IF NOT EXISTS approval_mode text NOT NULL DEFAULT 'admin_review';
ALTER TABLE identity_zone_join_requests ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE identity_zone_join_requests ADD COLUMN IF NOT EXISTS approved_by_user_id text REFERENCES identity_users (id);
DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'identity_zone_join_requests_approval_mode_check') THEN ALTER TABLE identity_zone_join_requests ADD CONSTRAINT identity_zone_join_requests_approval_mode_check CHECK (approval_mode IN ('admin_review', 'auto_no_admin')); END IF; END $$;
CREATE INDEX IF NOT EXISTS identity_zone_join_requests_tenant_status_idx ON identity_zone_join_requests (tenant_id, status);
CREATE TABLE IF NOT EXISTS identity_zone_admins (user_id text NOT NULL REFERENCES identity_users (id), tenant_id text NOT NULL REFERENCES identity_tenants (id), zone_id text NOT NULL REFERENCES identity_zones (id), status text NOT NULL CHECK (status IN ('active', 'revoked')), assigned_at timestamptz NOT NULL, PRIMARY KEY (user_id, zone_id));
CREATE INDEX IF NOT EXISTS identity_zone_admins_zone_status_idx ON identity_zone_admins (zone_id, status);
CREATE TABLE IF NOT EXISTS identity_zone_memberships (user_id text NOT NULL REFERENCES identity_users (id), tenant_id text NOT NULL REFERENCES identity_tenants (id), zone_id text NOT NULL REFERENCES identity_zones (id), status text NOT NULL CHECK (status IN ('active', 'revoked')), joined_at timestamptz NOT NULL, PRIMARY KEY (user_id, zone_id));
CREATE INDEX IF NOT EXISTS identity_zone_memberships_zone_status_idx ON identity_zone_memberships (zone_id, status);
CREATE TABLE IF NOT EXISTS identity_zone_creation_requests (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, user_id text NOT NULL REFERENCES identity_users (id), tenant_id text NOT NULL REFERENCES identity_tenants (id), requested_name text NOT NULL CHECK (char_length(requested_name) BETWEEN 2 AND 100), status text NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')), requested_at timestamptz NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS identity_zone_creation_requests_user_name_idx ON identity_zone_creation_requests (user_id, requested_name);
CREATE INDEX IF NOT EXISTS identity_zone_creation_requests_tenant_status_idx ON identity_zone_creation_requests (tenant_id, status);
CREATE TABLE IF NOT EXISTS identity_request_attempts (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, bucket_key bytea NOT NULL CHECK (octet_length(bucket_key) = 32), created_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_request_attempts_bucket_time_idx ON identity_request_attempts (bucket_key, created_at);
CREATE INDEX IF NOT EXISTS identity_request_attempts_time_idx ON identity_request_attempts (created_at);`

type config struct {
	environment, address, databaseURL, origin, authServiceURL string
	rateKey, internalKey                                      []byte
	proxies                                                   []netip.Prefix
}

func loadConfig(get func(string) string) (config, error) {
	c := config{environment: get("APPLICATION_IDENTITY_ENV"), address: get("APPLICATION_IDENTITY_LISTEN_ADDR"), databaseURL: get("APPLICATION_IDENTITY_DATABASE_URL"), origin: get("APPLICATION_IDENTITY_ORIGIN"), authServiceURL: strings.TrimSpace(get("APPLICATION_IDENTITY_AUTH_SERVICE_URL"))}
	if c.environment == "" {
		c.environment = "local"
	}
	if c.environment != "local" && c.environment != "test" && c.environment != "production" {
		return config{}, errors.New("APPLICATION_IDENTITY_ENV must be local, test, or production")
	}
	if c.address == "" {
		c.address = ":8080"
	}
	if c.authServiceURL == "" {
		return config{}, errors.New("APPLICATION_IDENTITY_AUTH_SERVICE_URL is required")
	}
	authURL, err := url.Parse(c.authServiceURL)
	if err != nil || (authURL.Scheme != "http" && authURL.Scheme != "https") || authURL.Host == "" || authURL.User != nil || authURL.Path != "" || authURL.RawQuery != "" || authURL.Fragment != "" {
		return config{}, errors.New("APPLICATION_IDENTITY_AUTH_SERVICE_URL must be an HTTP(S) origin without a path")
	}
	if c.databaseURL == "" {
		return config{}, errors.New("APPLICATION_IDENTITY_DATABASE_URL is required")
	}
	u, err := url.Parse(c.origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return config{}, errors.New("APPLICATION_IDENTITY_ORIGIN must be an HTTPS origin without a path")
	}
	entries := []struct {
		name string
		dst  *[]byte
	}{{"APPLICATION_IDENTITY_RATE_KEY", &c.rateKey}, {"APPLICATION_IDENTITY_INTERNAL_KEY", &c.internalKey}}
	for _, entry := range entries {
		decoded, err := base64.StdEncoding.DecodeString(get(entry.name))
		if err != nil || len(decoded) < 32 {
			return config{}, fmt.Errorf("%s must be base64-encoded and at least 32 bytes", entry.name)
		}
		*entry.dst = decoded
	}
	if subtle.ConstantTimeCompare(c.rateKey, c.internalKey) == 1 {
		return config{}, errors.New("identity internal key must be distinct from the rate key")
	}
	for _, raw := range strings.Split(get("APPLICATION_IDENTITY_TRUSTED_PROXIES"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || prefix.Bits() == 0 {
			return config{}, errors.New("APPLICATION_IDENTITY_TRUSTED_PROXIES contains an invalid or universal prefix")
		}
		c.proxies = append(c.proxies, prefix.Masked())
	}
	return c, nil
}

func main() {
	c, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := sql.Open("pgx", c.databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open identity database")
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var initialized atomic.Bool
	router := newInitializingHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if !initialized.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(checkCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/", router)
	listener, err := net.Listen("tcp", c.address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot bind HTTP listener")
		os.Exit(1)
	}
	server := &http.Server{Addr: c.address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	fmt.Fprintf(os.Stdout, "{\"level\":\"info\",\"service\":\"identity\",\"event\":\"service_started\",\"version\":%q,\"address\":%q,\"environment\":%q}\n", buildVersion, c.address, c.environment)
	if err := waitForSchema(ctx, db); err != nil {
		_ = server.Shutdown(context.Background())
		return
	}
	store, err := identity.NewPostgresStore(db, c.rateKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	authClient := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("auth service redirect rejected") }}
	service := identity.NewOnboardingService(store, c.origin, identity.HTTPPrincipalResolver{URL: c.authServiceURL, Client: authClient})
	limiter, err := identity.NewPostgresRequestLimiter(db, c.rateKey, 10*time.Minute, map[string]int{"/api/app/v1/auth/otp/challenges": 20, "/api/app/v1/auth/otp/verify": 50, "/api/app/v1/onboarding/nearby": 60, "/api/app/v1/onboarding/join": 20, "/api/app/v1/onboarding/zone-requests": 10})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	clientKey, err := identity.TrustedClientKey(c.proxies)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	auth := identity.HTTPHandler{Service: service, Limiter: limiter, ClientKey: clientKey, AllowedOrigin: c.origin, InternalKey: c.internalKey, AuthServiceURL: c.authServiceURL, AuthClient: authClient}
	router.Set(auth)
	initialized.Store(true)
	fmt.Fprintln(os.Stdout, "{\"level\":\"info\",\"service\":\"identity\",\"event\":\"database_ready\",\"message\":\"identity database ready\"}")
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	case err := <-errCh:
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}

func waitForSchema(ctx context.Context, db *sql.DB) error {
	return (servicehealth.RetryPolicy{
		InitialDelay:   time.Second,
		MaxDelay:       30 * time.Second,
		AttemptTimeout: 5 * time.Second,
	}).Wait(ctx, func(ctx context.Context) error {
		return ensureSchema(ctx, db)
	}, func(err error, retryIn time.Duration) {
		fmt.Fprintf(os.Stderr, "{\"level\":\"warn\",\"service\":\"identity\",\"event\":\"database_unavailable\",\"retry_in\":%q,\"error\":%q}\n", retryIn.String(), err.Error())
	})
}

type initializingHandler struct{ current atomic.Value }

func newInitializingHandler() *initializingHandler {
	h := &initializingHandler{}
	h.current.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	return h
}

func (h *initializingHandler) Set(next http.Handler) {
	h.current.Store(http.HandlerFunc(next.ServeHTTP))
}

func (h *initializingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.current.Load().(http.HandlerFunc).ServeHTTP(w, r)
}
