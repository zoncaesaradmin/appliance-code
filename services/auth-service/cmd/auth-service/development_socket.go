package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type developmentSocket struct {
	dir, path string
	listener  net.Listener
	server    *http.Server
}

func newDevelopmentSocket(environment, parent string, inbox *localInbox) (*developmentSocket, error) {
	if (environment != "local" && environment != "test") || inbox == nil || !filepath.IsAbs(parent) {
		return nil, errors.New("development inbox socket requires local or test configuration")
	}
	dir, err := os.MkdirTemp(parent, "zon-auth-otp-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "inbox.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(dir)
		return nil, err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/take" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var in struct {
			Phone string `json:"phone"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		code, err := inbox.take(in.Phone)
		if err != nil {
			http.Error(w, "code unavailable", 404)
			return
		}
		writeJSON(w, 200, map[string]string{"code": code})
	})
	return &developmentSocket{dir: dir, path: path, listener: listener, server: &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 4096}}, nil
}
func (s *developmentSocket) serve() error {
	err := s.server.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
func (s *developmentSocket) close() {
	if s == nil {
		return
	}
	_ = s.server.Close()
	_ = s.listener.Close()
	_ = os.Remove(s.path)
	_ = os.Remove(s.dir)
}
func takeDevelopmentCode(ctx context.Context, path, phone string) (string, error) {
	body, _ := json.Marshal(map[string]string{"phone": phone})
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/take", bytesReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("development inbox unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("development code unavailable")
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&out); err != nil {
		return "", err
	}
	return out.Code, nil
}
func bytesReader(b []byte) *byteReader { return &byteReader{data: b} }

type byteReader struct {
	data   []byte
	offset int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
func (_ *byteReader) Close() error { return nil }
