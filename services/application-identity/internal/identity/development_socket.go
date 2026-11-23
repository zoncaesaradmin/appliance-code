package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DevelopmentSocket exposes the in-memory inbox only through a Unix socket
// inside a newly created owner-only directory. It must never be put behind an
// ingress, TCP proxy, shared volume, or production workload.
type DevelopmentSocket struct {
	dir      string
	path     string
	listener net.Listener
	server   *http.Server
	once     sync.Once
	closeErr error
}

func NewDevelopmentSocket(environment string, inbox *DevelopmentInbox, parent string) (*DevelopmentSocket, error) {
	if (environment != "local" && environment != "test") || inbox == nil || !filepath.IsAbs(parent) {
		return nil, errors.New("development inbox socket requires local or test configuration")
	}
	dir, err := os.MkdirTemp(parent, "zonited-otp-")
	if err != nil {
		return nil, fmt.Errorf("create development socket directory: %w", err)
	}
	path := filepath.Join(dir, "inbox.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.Remove(dir)
		return nil, fmt.Errorf("create development socket: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(dir)
		return nil, fmt.Errorf("restrict development socket: %w", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/take" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var input struct {
			Phone string `json:"phone"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || !phonePattern.MatchString(input.Phone) {
			http.Error(w, "invalid request", 400)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			http.Error(w, "invalid request", 400)
			return
		}
		code, err := inbox.Take(r.Context(), input.Phone)
		if errors.Is(err, ErrDevelopmentCodeUnavailable) {
			http.Error(w, "code unavailable", 404)
			return
		}
		if err != nil {
			http.Error(w, "inbox unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Code string `json:"code"`
		}{Code: code})
	})
	return &DevelopmentSocket{dir: dir, path: path, listener: listener, server: &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096}}, nil
}

func (s *DevelopmentSocket) Path() string { return s.path }

// Serve blocks until the socket is closed. The caller owns the goroutine.
func (s *DevelopmentSocket) Serve() error {
	err := s.server.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *DevelopmentSocket) Close() error {
	s.once.Do(func() {
		serverErr := s.server.Close()
		listenerErr := s.listener.Close()
		if errors.Is(listenerErr, net.ErrClosed) {
			listenerErr = nil
		}
		s.closeErr = errors.Join(serverErr, listenerErr, os.Remove(s.dir))
	})
	return s.closeErr
}

// TakeDevelopmentCode is for a local operator process with access to the
// private socket directory. Callers must not log or persist the returned code.
func TakeDevelopmentCode(ctx context.Context, path, phone string) (string, error) {
	if !filepath.IsAbs(path) || !phonePattern.MatchString(phone) {
		return "", errors.New("invalid development inbox request")
	}
	body, err := json.Marshal(struct {
		Phone string `json:"phone"`
	}{Phone: phone})
	if err != nil {
		return "", err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/take", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("development inbox unavailable")
	}
	if resp.StatusCode == http.StatusNotFound {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return "", errors.New("development inbox unavailable")
		}
		return "", ErrDevelopmentCodeUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return "", errors.New("development inbox unavailable")
		}
		return "", errors.New("development inbox unavailable")
	}
	var result struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1024))
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&result)
	closeErr := resp.Body.Close()
	if decodeErr != nil || closeErr != nil || !codePattern.MatchString(result.Code) {
		return "", errors.New("invalid development inbox response")
	}
	return result.Code, nil
}
