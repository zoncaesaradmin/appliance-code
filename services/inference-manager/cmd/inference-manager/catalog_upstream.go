package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed list_vllm_archs.py
var vllmArchitectureProbe string

var libraryLink = regexp.MustCompile(`href="/library/([a-zA-Z0-9._:-]+)"`)
var sha1RE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// upstreamTransport is overridden in unit tests so discovery runs against a
// local TLS fixture instead of the public internet.
var upstreamTransport http.RoundTripper

func huggingfaceBase() string {
	return strings.TrimRight(env("INFERENCE_CATALOG_HF_BASE", "https://huggingface.co"), "/")
}

func ollamaLibraryBase() string {
	return strings.TrimRight(env("INFERENCE_CATALOG_OLLAMA_BASE", "https://ollama.com"), "/")
}

func ollamaRegistryBase() string {
	return strings.TrimRight(env("INFERENCE_CATALOG_OLLAMA_REGISTRY_BASE", "https://registry.ollama.ai"), "/")
}

func upstreamClient() *http.Client {
	transport := upstreamTransport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &http.Client{Timeout: 20 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("unexpected catalog redirect")
		}
		return nil
	}}
}

func upstreamRead(ctx context.Context, address string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "appliance-model-catalog/1")
	resp, err := upstreamClient().Do(req)
	if err != nil {
		return fmt.Errorf("upstream metadata unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream metadata returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("upstream metadata exceeds size limit")
	}
	if text, ok := target.(*string); ok {
		*text = string(b)
		return nil
	}
	return json.Unmarshal(b, target)
}

func libraryReferences(page string, tags bool, limit int) []string {
	seen := map[string]bool{}
	var result []string
	for _, match := range libraryLink.FindAllStringSubmatch(page, -1) {
		ref := match[1]
		if strings.Contains(ref, ":") != tags || strings.HasSuffix(ref, ":latest") || strings.Contains(ref, "cloud") || seen[ref] {
			continue
		}
		seen[ref] = true
		result = append(result, ref)
		if len(result) == limit {
			break
		}
	}
	return result
}

func (m *manager) discoverModels(ctx context.Context) ([]catalogEntry, error) {
	if m.engine == "ollama" {
		return discoverOllama(ctx)
	}
	return m.discoverVLLM(ctx)
}

func discoverOllama(ctx context.Context) ([]catalogEntry, error) {
	var page string
	if err := upstreamRead(ctx, ollamaLibraryBase()+"/library?sort=popular", &page); err != nil {
		return nil, err
	}
	families := libraryReferences(page, false, 24)
	if len(families) == 0 {
		return nil, fmt.Errorf("Ollama library format was not recognized")
	}
	var entries []catalogEntry
	for _, family := range families {
		if err := upstreamRead(ctx, ollamaLibraryBase()+"/library/"+family+"/tags", &page); err != nil {
			return nil, err
		}
		refs := libraryReferences(page, true, 8)
		for _, ref := range refs {
			parts := strings.SplitN(ref, ":", 2)
			var manifest struct {
				Layers []struct {
					Size      uint64 `json:"size"`
					MediaType string `json:"mediaType"`
				} `json:"layers"`
			}
			if err := upstreamRead(ctx, ollamaRegistryBase()+"/v2/library/"+parts[0]+"/manifests/"+parts[1], &manifest); err != nil {
				return nil, err
			}
			var weights, total uint64
			for _, layer := range manifest.Layers {
				total += layer.Size
				if layer.MediaType == "application/vnd.ollama.image.model" {
					weights += layer.Size
				}
			}
			if weights == 0 || weights > 1<<40 || total > 1<<40 {
				continue
			}
			entries = append(entries, catalogEntry{ID: ref, Source: ref, DownloadBytes: total, MemoryBytes: weights*2 + (2 << 30)})
		}
	}
	return entries, nil
}

func pythonOutput(ctx context.Context, timeout time.Duration, stdin string, args ...string) ([]byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, env("INFERENCE_PYTHON", "python3"), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 2048 {
			detail = detail[len(detail)-2048:]
		}
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func installedVLLMArchitectures(ctx context.Context) (map[string]bool, error) {
	// Fast path: engine sidecar (or test) already published the architecture list.
	if supported, err := readVLLMArchitecturesFile(vllmArchitecturesFile()); err == nil {
		return supported, nil
	}
	// Local package probe for tests and legacy mono-images. Importing ModelRegistry
	// initializes CUDA/platform plugins and fails in Restricted pods, so the probe
	// parses registry.py without importing vLLM.
	b, err := pythonOutput(ctx, 30*time.Second, vllmArchitectureProbe, "-")
	if err == nil {
		return decodeVLLMArchitecturesJSON(b)
	}
	probeErr := err
	// Dual-image layout: thin manager has no vLLM package. Wait for the engine
	// Prefer architectures published by the install Job to
	// /models/.zon/vllm-architectures.json.
	if supported, waitErr := waitVLLMArchitecturesFile(ctx); waitErr == nil {
		return supported, nil
	}
	return nil, fmt.Errorf("cannot determine installed vLLM model architectures: %w", probeErr)
}

func vllmArchitecturesFile() string {
	if path := strings.TrimSpace(env("INFERENCE_VLLM_ARCHITECTURES_FILE", "")); path != "" {
		return path
	}
	if path := strings.TrimSpace(env("INFERENCE_ARCH_FILE", "")); path != "" {
		return path
	}
	// Prefer models PVC publish path from the arch-probe Job; fall back to legacy control dir.
	modelsDir := env("INFERENCE_MODELS_DIR", "/models")
	return filepath.Join(modelsDir, ".zon", "vllm-architectures.json")
}

func waitVLLMArchitecturesFile(ctx context.Context) (map[string]bool, error) {
	path := vllmArchitecturesFile()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for {
		supported, err := readVLLMArchitecturesFile(path)
		if err == nil {
			return supported, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func readVLLMArchitecturesFile(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeVLLMArchitecturesJSON(b)
}

func decodeVLLMArchitecturesJSON(b []byte) (map[string]bool, error) {
	var architectures []string
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &architectures); err != nil {
		return nil, fmt.Errorf("cannot determine installed vLLM model architectures: %w", err)
	}
	if len(architectures) == 0 {
		return nil, fmt.Errorf("cannot determine installed vLLM model architectures: empty architecture list")
	}
	supported := map[string]bool{}
	for _, architecture := range architectures {
		supported[architecture] = true
	}
	return supported, nil
}

func (m *manager) discoverVLLM(ctx context.Context) ([]catalogEntry, error) {
	supported, err := installedVLLMArchitectures(ctx)
	if err != nil {
		return nil, err
	}
	var models []struct {
		ID      string `json:"id"`
		SHA     string `json:"sha"`
		Gated   any    `json:"gated"`
		Private bool   `json:"private"`
	}
	if err := upstreamRead(ctx, huggingfaceBase()+"/api/models?filter=text-generation&sort=downloads&direction=-1&limit=40&full=true", &models); err != nil {
		return nil, err
	}
	var entries []catalogEntry
	for _, item := range models {
		if item.Private || (item.Gated != nil && item.Gated != false) || !modelRefRE.MatchString(item.ID) || !sha1RE.MatchString(item.SHA) {
			continue
		}
		var config struct {
			Architectures         []string        `json:"architectures"`
			Quantization          json.RawMessage `json:"quantization_config"`
			AutoMap               json.RawMessage `json:"auto_map"`
			MaxPositionEmbeddings json.Number     `json:"max_position_embeddings"`
			NPositions            json.Number     `json:"n_positions"`
			NumHiddenLayers       json.Number     `json:"num_hidden_layers"`
			NLayer                json.Number     `json:"n_layer"`
			NumKeyValueHeads      json.Number     `json:"num_key_value_heads"`
			NumAttentionHeads     json.Number     `json:"num_attention_heads"`
			NHead                 json.Number     `json:"n_head"`
			HiddenSize            json.Number     `json:"hidden_size"`
			NEmbd                 json.Number     `json:"n_embd"`
		}
		address := huggingfaceBase() + "/" + item.ID + "/resolve/" + item.SHA + "/config.json"
		// Hugging Face resolve redirects to its same-host metadata cache.
		if err := upstreamRead(ctx, address, &config); err != nil {
			continue
		}
		compatible := false
		for _, architecture := range config.Architectures {
			compatible = compatible || supported[architecture]
		}
		// Quantized/custom-code models need mode-specific recipes; do not
		// automatically recommend them based on architecture alone.
		if !compatible || len(config.Quantization) > 0 || len(config.AutoMap) > 0 {
			continue
		}
		raw, _ := json.Marshal(config)
		arch := modelArchFromConfigJSON(raw)
		modelLimit := arch.MaxPosition
		if modelLimit == 0 {
			modelLimit = maxPositionFromNumbers(config.MaxPositionEmbeddings, config.NPositions)
			arch.MaxPosition = modelLimit
		}
		var info struct {
			Siblings []struct {
				Name string `json:"rfilename"`
				Size uint64 `json:"size"`
			} `json:"siblings"`
		}
		if err := upstreamRead(ctx, huggingfaceBase()+"/api/models/"+item.ID+"/revision/"+url.PathEscape(item.SHA)+"?blobs=true", &info); err != nil {
			return nil, err
		}
		var total, weights uint64
		for _, file := range info.Siblings {
			total += file.Size
			if strings.HasSuffix(file.Name, ".safetensors") {
				weights += file.Size
			}
		}
		if weights == 0 || total > 1<<40 || weights > 1<<40 {
			continue
		}
		// LaunchArguments are filled at catalog snapshot time from planServe so
		// they always reflect current mode + host memory, not discovery-time guess.
		entries = append(entries, catalogEntry{
			ID:                item.ID,
			Source:            item.ID + "@" + item.SHA,
			DownloadBytes:     total,
			MemoryBytes:       weights*2 + (4 << 30),
			ModelContextLimit: modelLimit,
			KVBytesPerToken:   arch.kvBytesPerToken(),
		})
	}
	return entries, nil
}
