// Package inference owns the appliance-facing inference runtime and model
// lifecycle contract. Engine-specific APIs stay behind this package.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	modelReferenceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,511}$`)
	digestRE         = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

var (
	ErrInvalidRequest = errors.New("inference: invalid request")
	ErrUnsupported    = errors.New("inference: operation unsupported by runtime")
	ErrUnavailable    = errors.New("inference: runtime unavailable")
	ErrBusy           = errors.New("inference: another model operation is in progress")
)

type Config struct {
	BaseURL        string
	Package        string
	Engine         string
	Architecture   string
	SupportedModes []string
	RequestedMode  string
}

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type RuntimeCapabilities struct {
	Package          string   `json:"package"`
	Engine           string   `json:"engine"`
	Architecture     string   `json:"architecture"`
	HostArchitecture string   `json:"hostArchitecture"`
	SupportedModes   []string `json:"supportedModes"`
	RequestedMode    string   `json:"requestedMode"`
	ActiveMode       string   `json:"activeMode,omitempty"`
	Checks           []Check  `json:"checks"`
}

type RuntimeStatus struct {
	RuntimeCapabilities
	Ready bool `json:"ready"`
}

type Model struct {
	ID      string         `json:"id"`
	Object  string         `json:"object,omitempty"`
	OwnedBy string         `json:"ownedBy,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type ImportRequest struct {
	ModelID         string   `json:"modelId"`
	Source          string   `json:"source"`
	Digest          string   `json:"digest,omitempty"`
	LaunchArguments []string `json:"launchArguments,omitempty"`
}

type Service struct {
	cfg            Config
	base           *url.URL
	client         *http.Client
	modelOperation sync.Mutex
}

func New(cfg Config, client *http.Client) (*Service, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || base.Scheme == "" || base.Host == "" || base.Path != "" {
		return nil, fmt.Errorf("inference: base URL must be absolute with no path")
	}
	cfg.Engine = strings.ToLower(strings.TrimSpace(cfg.Engine))
	cfg.RequestedMode = strings.ToLower(strings.TrimSpace(cfg.RequestedMode))
	if cfg.RequestedMode == "" {
		cfg.RequestedMode = "auto"
	}
	if cfg.Engine != "ollama" && cfg.Engine != "vllm" {
		return nil, fmt.Errorf("inference: unsupported engine %q", cfg.Engine)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	return &Service{cfg: cfg, base: base, client: client}, nil
}

func (s *Service) Capabilities(ctx context.Context) RuntimeCapabilities {
	active := ""
	checks := []Check{{Name: "architecture", Status: "pass"}}
	hostArch := normalizeArchitecture(runtime.GOARCH)
	if want := normalizeArchitecture(s.cfg.Architecture); want != "" && want != hostArch {
		checks[0] = Check{Name: "architecture", Status: "fail", Message: fmt.Sprintf("runtime package targets %s; process architecture is %s", want, hostArch)}
	}
	requestedSupported := s.cfg.RequestedMode == "auto" || contains(s.cfg.SupportedModes, s.cfg.RequestedMode)
	if requestedSupported && s.cfg.Engine == "ollama" && (s.cfg.RequestedMode == "auto" || s.cfg.RequestedMode == "cpu") && contains(s.cfg.SupportedModes, "cpu") {
		// The current Ollama package is CPU-only, so this is deterministic.
		// Multi-mode runtimes must report their selected mode from the runtime
		// capability endpoint; never infer CUDA from architecture or a vendor.
		active = "cpu"
	} else if requestedSupported {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var detected struct {
			AvailableModes []string `json:"availableModes"`
			ActiveMode     string   `json:"activeMode"`
			Checks         []Check  `json:"checks"`
		}
		err := s.getJSON(probeCtx, "/internal/v1/runtime/capabilities", &detected)
		detectedMode := strings.ToLower(strings.TrimSpace(detected.ActiveMode))
		modeMatchesRequest := s.cfg.RequestedMode == "auto" || detectedMode == s.cfg.RequestedMode
		if err == nil && contains(s.cfg.SupportedModes, detectedMode) && contains(detected.AvailableModes, detectedMode) && modeMatchesRequest {
			active = detectedMode
			checks = append(checks, detected.Checks...)
		} else {
			checks = append(checks, Check{Name: "runtime-detection", Status: "pending", Message: "runtime has not confirmed a compatible active CPU or CUDA mode"})
		}
	}
	if active == "" {
		checks = append(checks, Check{Name: "mode", Status: "fail", Message: "requested mode is not supported by the selected runtime package"})
	} else {
		checks = append(checks, Check{Name: "mode", Status: "pass", Message: active})
	}
	return RuntimeCapabilities{Package: s.cfg.Package, Engine: s.cfg.Engine, Architecture: s.cfg.Architecture, HostArchitecture: hostArch, SupportedModes: append([]string(nil), s.cfg.SupportedModes...), RequestedMode: s.cfg.RequestedMode, ActiveMode: active, Checks: checks}
}

func (s *Service) getJSON(ctx context.Context, path string, target any) error {
	resp, err := s.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
}

func (s *Service) Status(ctx context.Context) RuntimeStatus {
	capabilities := s.Capabilities(ctx)
	ready := false
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := s.do(statusCtx, http.MethodGet, "/v1/models", nil)
	if err == nil {
		ready = resp.StatusCode >= 200 && resp.StatusCode < 300
		resp.Body.Close()
	}
	status := "pass"
	message := "OpenAI-compatible runtime API is reachable"
	if !ready {
		status, message = "fail", "OpenAI-compatible runtime API is unavailable"
	}
	capabilities.Checks = append(capabilities.Checks, Check{Name: "runtime-api", Status: status, Message: message})
	return RuntimeStatus{RuntimeCapabilities: capabilities, Ready: ready}
}

func (s *Service) ListModels(ctx context.Context) ([]Model, error) {
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := s.do(listCtx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, responseError(resp)
	}
	var body struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("inference: decode models: %w", err)
	}
	if body.Data == nil {
		body.Data = []Model{}
	}
	return body.Data, nil
}

func (s *Service) Import(ctx context.Context, req ImportRequest) error {
	if !s.modelOperation.TryLock() {
		return ErrBusy
	}
	defer s.modelOperation.Unlock()
	req.ModelID = strings.TrimSpace(req.ModelID)
	req.Source = strings.TrimSpace(req.Source)
	if req.ModelID == "" || req.Source == "" {
		return fmt.Errorf("%w: modelId and source are required", ErrInvalidRequest)
	}
	if !modelReferenceRE.MatchString(req.ModelID) || !modelReferenceRE.MatchString(req.Source) {
		return fmt.Errorf("%w: modelId and source must be engine model references, not URLs or filesystem paths", ErrInvalidRequest)
	}
	if req.Digest != "" && !digestRE.MatchString(strings.TrimSpace(req.Digest)) {
		return fmt.Errorf("%w: digest must be a lowercase sha256 digest", ErrInvalidRequest)
	}
	if err := validateLaunchArguments(s.cfg.Engine, req.LaunchArguments); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if s.cfg.Engine == "ollama" {
		if req.ModelID != req.Source {
			return fmt.Errorf("%w: Ollama modelId must equal source", ErrInvalidRequest)
		}
		if req.Digest != "" {
			return fmt.Errorf("%w: expected-digest verification requires an inference manager and is not available for direct Ollama pulls", ErrUnsupported)
		}
	}
	return s.callJSON(ctx, http.MethodPost, "/internal/v1/models/imports", req)
}

func validateLaunchArguments(engine string, arguments []string) error {
	if engine == "ollama" {
		if len(arguments) != 0 {
			return errors.New("launchArguments are only valid for vLLM")
		}
		return nil
	}
	if len(arguments) > 32 {
		return errors.New("launchArguments may contain at most 32 entries")
	}
	valueArguments := map[string]func(string) bool{
		"--quantization":            func(v string) bool { return v != "" && !strings.HasPrefix(v, "-") },
		"--max-model-len":           func(v string) bool { n, err := strconv.ParseUint(v, 10, 64); return err == nil && n > 0 },
		"--gpu-memory-utilization":  func(v string) bool { n, err := strconv.ParseFloat(v, 64); return err == nil && n > 0 && n <= 1 },
		"--cudagraph-capture-sizes": func(v string) bool { n, err := strconv.ParseUint(v, 10, 64); return err == nil && n > 0 },
		"--served-model-name":       func(v string) bool { return modelReferenceRE.MatchString(v) },
		"--tool-call-parser":        func(v string) bool { return v != "" && !strings.HasPrefix(v, "-") },
	}
	booleanArguments := map[string]bool{"--no-enable-flashinfer-autotune": true, "--enable-auto-tool-choice": true}
	seen := map[string]bool{}
	for index := 0; index < len(arguments); index++ {
		argument := strings.TrimSpace(arguments[index])
		if validator, ok := valueArguments[argument]; ok {
			if seen[argument] || index+1 >= len(arguments) || !validator(strings.TrimSpace(arguments[index+1])) {
				return fmt.Errorf("invalid or duplicate vLLM argument %s", argument)
			}
			seen[argument] = true
			index++
			continue
		}
		if booleanArguments[argument] && !seen[argument] {
			seen[argument] = true
			continue
		}
		return fmt.Errorf("unsupported vLLM launch argument %q", argument)
	}
	return nil
}

func (s *Service) Load(ctx context.Context, modelID string) error {
	if !s.modelOperation.TryLock() {
		return ErrBusy
	}
	defer s.modelOperation.Unlock()
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("%w: model id is required", ErrInvalidRequest)
	}
	return s.callJSON(ctx, http.MethodPost, "/internal/v1/models/"+url.PathEscape(modelID)+"/load", map[string]any{})
}

func (s *Service) Delete(ctx context.Context, modelID string) error {
	if !s.modelOperation.TryLock() {
		return ErrBusy
	}
	defer s.modelOperation.Unlock()
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("%w: model id is required", ErrInvalidRequest)
	}
	return s.callJSON(ctx, http.MethodDelete, "/internal/v1/models/"+url.PathEscape(modelID), nil)
}

func (s *Service) callJSON(ctx context.Context, method, path string, body any) error {
	resp, err := s.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	return nil
}

func (s *Service) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	target := *s.base
	target.Path = path
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return resp, nil
}

func responseError(resp *http.Response) error {
	message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("inference: runtime returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func normalizeArchitecture(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "x86_64", "x86-64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
