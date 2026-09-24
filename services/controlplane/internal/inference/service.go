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
	ErrInvalidRequest   = errors.New("inference: invalid request")
	ErrUnsupported      = errors.New("inference: operation unsupported by runtime")
	ErrUnavailable      = errors.New("inference: runtime unavailable")
	ErrBusy             = errors.New("inference: another model operation is in progress")
	ErrAlreadyInstalled = errors.New("inference: model is already installed")
	ErrConflict         = errors.New("inference: conflict")
)

type Config struct {
	BaseURL      string
	Package      string
	Engine       string
	Architecture string
}

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type RuntimeCapabilities struct {
	Package          string  `json:"package"`
	Engine           string  `json:"engine"`
	Architecture     string  `json:"architecture"`
	HostArchitecture string  `json:"hostArchitecture"`
	Acceleration     string  `json:"acceleration"` // standard | accelerated
	GPUAvailable     *bool   `json:"gpuAvailable,omitempty"`
	Checks           []Check `json:"checks"`
}

type RuntimeStatus struct {
	RuntimeCapabilities
	Ready         bool   `json:"ready"`
	LoadedModelID string `json:"loadedModelId,omitempty"`
	ServingState  string `json:"servingState,omitempty"` // inactive|loading|ready|failed
	// Instances is the manager's desired serving inventory. The current
	// appliance still reconciles only the default instance, but this list keeps
	// the control-plane/UI contract independent of that singleton implementation.
	Instances []ServingInstance `json:"instances"`
	// MaxModelLen is the engine's served context window (--max-model-len /
	// max_model_len), not the raw model-card limit. Client copy settings must
	// use this when ready so Codex is not told 32768 while the engine is at 8192.
	MaxModelLen uint64 `json:"maxModelLen,omitempty"`
}

type ServingInstance struct {
	ID       string   `json:"id"`
	Models   []string `json:"models"`
	Replicas int32    `json:"replicas"`
}

type Model struct {
	ID              string            `json:"id"`
	SizeBytes       uint64            `json:"sizeBytes,omitempty"`
	Object          string            `json:"object,omitempty"`
	OwnedBy         string            `json:"ownedBy,omitempty"`
	Source          string            `json:"source,omitempty"`
	Digest          string            `json:"digest,omitempty"`
	LaunchArguments []string          `json:"launchArguments,omitempty"`
	Details         map[string]any    `json:"details,omitempty"`
	Capabilities    ModelCapabilities `json:"capabilities"`
}

type ModelCapabilities struct {
	Experiences         []string `json:"experiences"`
	ToolCalling         bool     `json:"toolCalling"`
	ResponsesCompatible bool     `json:"responsesCompatible"`
	CodexCompatible     bool     `json:"codexCompatible"`
	Verification        string   `json:"verification"`
}

type ImportRequest struct {
	CatalogID       string   `json:"catalogId,omitempty"`
	ModelID         string   `json:"modelId"`
	Source          string   `json:"source"`
	Digest          string   `json:"digest,omitempty"`
	LaunchArguments []string `json:"launchArguments,omitempty"`
}

type ImportProgress struct {
	ModelID         string `json:"modelId,omitempty"`
	Source          string `json:"source,omitempty"`
	State           string `json:"state"`
	BytesDownloaded uint64 `json:"bytesDownloaded,omitempty"`
	BytesTotal      uint64 `json:"bytesTotal,omitempty"`
	Percent         *int   `json:"percent,omitempty"`
	Message         string `json:"message,omitempty"`
	Error           string `json:"error,omitempty"`
	UpdatedAt       string `json:"updatedAt,omitempty"`
}

type LoadProgress struct {
	ModelID     string `json:"modelId,omitempty"`
	State       string `json:"state"` // idle|loading|ready|failed
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
	EnginePhase string `json:"enginePhase,omitempty"`
	OOMKilled   bool   `json:"oomKilled,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// ChatMessage is the narrow OpenAI chat-completions shape used by the native
// appliance chat surface. The model is selected by the control plane, never
// by the browser.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// Catalog is runtime-owned and cached locally; reading it never refreshes upstream.
func (s *Service) Catalog(ctx context.Context, sortBy, order string) (json.RawMessage, error) {
	sortBy = strings.TrimSpace(sortBy)
	order = strings.TrimSpace(order)
	if sortBy != "" {
		switch strings.ToLower(sortBy) {
		case "parameters", "memory", "name":
		default:
			return nil, fmt.Errorf("%w: unsupported sort %q; use parameters, memory, or name", ErrInvalidRequest, sortBy)
		}
	}
	if order != "" {
		switch strings.ToLower(order) {
		case "asc", "desc":
		default:
			return nil, fmt.Errorf("%w: unsupported order %q; use asc or desc", ErrInvalidRequest, order)
		}
	}
	path := "/internal/v1/models/catalog"
	query := url.Values{}
	if sortBy != "" {
		query.Set("sort", sortBy)
	}
	if order != "" {
		query.Set("order", order)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var catalog json.RawMessage
	err := s.getJSON(ctx, path, &catalog)
	return catalog, err
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
	if cfg.Engine != "ollama" && cfg.Engine != "vllm" {
		return nil, fmt.Errorf("inference: unsupported engine %q", cfg.Engine)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	return &Service{cfg: cfg, base: base, client: client}, nil
}

func (s *Service) Capabilities(ctx context.Context) RuntimeCapabilities {
	checks := []Check{{Name: "architecture", Status: "pass"}}
	hostArch := normalizeArchitecture(runtime.GOARCH)
	if want := normalizeArchitecture(s.cfg.Architecture); want != "" && want != hostArch {
		checks[0] = Check{Name: "architecture", Status: "fail", Message: fmt.Sprintf("runtime package targets %s; process architecture is %s", want, hostArch)}
	}
	acceleration := "standard"
	if s.cfg.Engine == "vllm" {
		acceleration = "accelerated"
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var detected struct {
		GPUAvailable bool    `json:"gpuAvailable"`
		Checks       []Check `json:"checks"`
	}
	var gpuAvailable *bool
	err := s.getJSON(probeCtx, "/internal/v1/runtime/capabilities", &detected)
	if err == nil {
		value := detected.GPUAvailable
		gpuAvailable = &value
		checks = append(checks, detected.Checks...)
		if s.cfg.Engine == "vllm" && !detected.GPUAvailable {
			checks = append(checks, Check{Name: "gpu", Status: "fail", Message: "accelerated inference requires a usable GPU"})
		} else if s.cfg.Engine == "vllm" {
			checks = append(checks, Check{Name: "gpu", Status: "pass", Message: "GPU available for accelerated inference"})
		} else {
			checks = append(checks, Check{Name: "runtime", Status: "pass", Message: "standard inference runtime ready"})
		}
	} else {
		checks = append(checks, Check{Name: "runtime-detection", Status: "pending", Message: "runtime has not confirmed hardware readiness"})
	}

	return RuntimeCapabilities{
		Package:          s.cfg.Package,
		Engine:           s.cfg.Engine,
		Architecture:     s.cfg.Architecture,
		HostArchitecture: hostArch,
		Acceleration:     acceleration,
		GPUAvailable:     gpuAvailable,
		Checks:           checks,
	}
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
	var health struct {
		MaxModelLen uint64            `json:"maxModelLen"`
		Instances   []ServingInstance `json:"instances"`
	}
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Probe the manager admin health surface. OpenAI /v1/* is only up when a
	// model is loaded; idle appliances must still report the manager as ready.
	resp, err := s.do(statusCtx, http.MethodGet, "/", nil)
	if err == nil {
		ready = resp.StatusCode >= 200 && resp.StatusCode < 300
		if ready {
			_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&health)
		}
		resp.Body.Close()
	}
	status := "pass"
	message := "Inference manager is reachable"
	if !ready {
		status, message = "fail", "Inference manager is unavailable"
	}
	capabilities.Checks = append(capabilities.Checks, Check{Name: "runtime-api", Status: status, Message: message})

	servingState := "inactive"
	loadedModelID := ""
	if progress, progressErr := s.LoadProgress(ctx); progressErr == nil {
		switch progress.State {
		case "loading":
			servingState = "loading"
			loadedModelID = progress.ModelID
		case "ready":
			servingState = "ready"
			loadedModelID = progress.ModelID
		case "failed":
			servingState = "failed"
			loadedModelID = progress.ModelID
		}
	}
	return RuntimeStatus{
		RuntimeCapabilities: capabilities,
		Ready:               ready,
		LoadedModelID:       loadedModelID,
		ServingState:        servingState,
		Instances:           health.Instances,
		MaxModelLen:         health.MaxModelLen,
	}
}

func (s *Service) ListModels(ctx context.Context) ([]Model, error) {
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Downloaded inventory is manager-owned; /v1/models is the OpenAI engine proxy.
	resp, err := s.do(listCtx, http.MethodGet, "/internal/v1/models", nil)
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

// StreamChat opens the selected runtime's OpenAI-compatible streaming
// endpoint. Callers own the returned body and must close it.
func (s *Service) StreamChat(ctx context.Context, request ChatRequest) (*http.Response, error) {
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 {
		return nil, fmt.Errorf("%w: model and messages are required", ErrInvalidRequest)
	}
	request.Stream = true
	return s.do(ctx, http.MethodPost, "/v1/chat/completions", request)
}

func (s *Service) Import(ctx context.Context, req ImportRequest) (ImportProgress, error) {
	if !s.modelOperation.TryLock() {
		return ImportProgress{}, ErrBusy
	}
	defer s.modelOperation.Unlock()
	req.ModelID = strings.TrimSpace(req.ModelID)
	req.Source = strings.TrimSpace(req.Source)
	if req.ModelID == "" || req.Source == "" {
		return ImportProgress{}, fmt.Errorf("%w: modelId and source are required", ErrInvalidRequest)
	}
	if !modelReferenceRE.MatchString(req.ModelID) || !modelReferenceRE.MatchString(req.Source) {
		return ImportProgress{}, fmt.Errorf("%w: modelId and source must be engine model references, not URLs or filesystem paths", ErrInvalidRequest)
	}
	if req.Digest != "" && !digestRE.MatchString(strings.TrimSpace(req.Digest)) {
		return ImportProgress{}, fmt.Errorf("%w: digest must be a lowercase sha256 digest", ErrInvalidRequest)
	}
	if err := validateLaunchArguments(s.cfg.Engine, req.LaunchArguments); err != nil {
		return ImportProgress{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if s.cfg.Engine == "ollama" {
		if req.ModelID != req.Source {
			return ImportProgress{}, fmt.Errorf("%w: Ollama modelId must equal source", ErrInvalidRequest)
		}
		if req.Digest != "" {
			return ImportProgress{}, fmt.Errorf("%w: expected-digest verification requires an inference manager and is not available for direct Ollama pulls", ErrUnsupported)
		}
	}
	var progress ImportProgress
	if err := s.callJSON(ctx, http.MethodPost, "/internal/v1/models/imports", req, &progress); err != nil {
		return ImportProgress{}, err
	}
	if progress.State == "" {
		progress.State = "downloading"
		progress.ModelID = req.ModelID
		progress.Source = req.Source
	}
	return progress, nil
}

func (s *Service) ImportProgress(ctx context.Context) (ImportProgress, error) {
	progressCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var progress ImportProgress
	if err := s.getJSON(progressCtx, "/internal/v1/models/imports/progress", &progress); err != nil {
		return ImportProgress{}, err
	}
	if progress.State == "" {
		progress.State = "idle"
	}
	return progress, nil
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

func (s *Service) Load(ctx context.Context, modelID string) (LoadProgress, error) {
	if !s.modelOperation.TryLock() {
		return LoadProgress{}, ErrBusy
	}
	defer s.modelOperation.Unlock()
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return LoadProgress{}, fmt.Errorf("%w: model id is required", ErrInvalidRequest)
	}
	var progress LoadProgress
	if err := s.callJSON(ctx, http.MethodPost, "/internal/v1/models/load", map[string]any{"modelId": modelID}, &progress); err != nil {
		return LoadProgress{}, err
	}
	if progress.State == "" {
		progress.State = "loading"
		progress.ModelID = modelID
	}
	return progress, nil
}

func (s *Service) LoadProgress(ctx context.Context) (LoadProgress, error) {
	progressCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var progress LoadProgress
	if err := s.getJSON(progressCtx, "/internal/v1/models/load/progress", &progress); err != nil {
		return LoadProgress{}, err
	}
	if progress.State == "" {
		progress.State = "idle"
	}
	return progress, nil
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
	return s.callJSON(ctx, http.MethodPost, "/internal/v1/models/delete", map[string]any{"modelId": modelID}, nil)
}

func (s *Service) callJSON(ctx context.Context, method, path string, body any, target any) error {
	resp, err := s.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
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
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("inference: invalid path %q: %w", path, err)
	}
	target := s.base.ResolveReference(rel)
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
	text := strings.TrimSpace(string(message))
	detail := managerErrorMessage(text)
	lower := strings.ToLower(detail)
	switch resp.StatusCode {
	case http.StatusConflict:
		switch {
		case strings.Contains(lower, "another model operation is in progress"):
			return fmt.Errorf("%w: %s", ErrBusy, detail)
		case strings.Contains(lower, "model is already installed"):
			return fmt.Errorf("%w: %s", ErrAlreadyInstalled, detail)
		default:
			return fmt.Errorf("%w: %s", ErrConflict, detail)
		}
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrInvalidRequest, detail)
	default:
		return fmt.Errorf("inference: runtime returned %d: %s", resp.StatusCode, text)
	}
}

func managerErrorMessage(body string) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return msg
		}
	}
	return body
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
