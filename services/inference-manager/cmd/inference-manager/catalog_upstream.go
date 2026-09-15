package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var libraryLink = regexp.MustCompile(`href="/library/([a-zA-Z0-9._:-]+)"`)

func upstreamRead(ctx context.Context, address string, target any) error {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("unexpected catalog redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "appliance-model-catalog/1")
	resp, err := client.Do(req)
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
	if err := upstreamRead(ctx, "https://ollama.com/library?sort=popular", &page); err != nil {
		return nil, err
	}
	families := libraryReferences(page, false, 24)
	if len(families) == 0 {
		return nil, fmt.Errorf("Ollama library format was not recognized")
	}
	var entries []catalogEntry
	for _, family := range families {
		if err := upstreamRead(ctx, "https://ollama.com/library/"+family+"/tags", &page); err != nil {
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
			if err := upstreamRead(ctx, "https://registry.ollama.ai/v2/library/"+parts[0]+"/manifests/"+parts[1], &manifest); err != nil {
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

func (m *manager) discoverVLLM(ctx context.Context) ([]catalogEntry, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(probeCtx, env("INFERENCE_PYTHON", "python3"), "-c", "import json; from vllm import ModelRegistry; print(json.dumps(list(ModelRegistry.get_supported_archs())))").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot determine installed vLLM model architectures: %w", err)
	}
	var architectures []string
	// Runtime imports may print informational lines before the JSON.
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &architectures); err != nil {
		return nil, err
	}
	supported := map[string]bool{}
	for _, architecture := range architectures {
		supported[architecture] = true
	}
	var models []struct {
		ID      string `json:"id"`
		SHA     string `json:"sha"`
		Gated   any    `json:"gated"`
		Private bool   `json:"private"`
	}
	if err := upstreamRead(ctx, "https://huggingface.co/api/models?filter=text-generation&sort=downloads&direction=-1&limit=40&full=true", &models); err != nil {
		return nil, err
	}
	var entries []catalogEntry
	for _, item := range models {
		if item.Private || (item.Gated != nil && item.Gated != false) || !modelRefRE.MatchString(item.ID) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(item.SHA) {
			continue
		}
		var config struct {
			Architectures []string        `json:"architectures"`
			Quantization  json.RawMessage `json:"quantization_config"`
			AutoMap       json.RawMessage `json:"auto_map"`
		}
		address := "https://huggingface.co/" + item.ID + "/resolve/" + item.SHA + "/config.json"
		// Hugging Face resolve redirects to its same-host metadata cache.
		if err := upstreamRead(ctx, address, &config); err != nil {
			continue
		}
		compatible := false
		for _, arch := range config.Architectures {
			compatible = compatible || supported[arch]
		}
		// Quantized/custom-code models need mode-specific recipes; do not
		// automatically recommend them based on architecture alone.
		if !compatible || len(config.Quantization) > 0 || len(config.AutoMap) > 0 {
			continue
		}
		var info struct {
			Siblings []struct {
				Name string `json:"rfilename"`
				Size uint64 `json:"size"`
			} `json:"siblings"`
		}
		if err := upstreamRead(ctx, "https://huggingface.co/api/models/"+item.ID+"/revision/"+url.PathEscape(item.SHA)+"?blobs=true", &info); err != nil {
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
		entries = append(entries, catalogEntry{ID: item.ID, Source: item.ID + "@" + item.SHA, DownloadBytes: total, MemoryBytes: weights*2 + (4 << 30), LaunchArguments: []string{"--max-model-len", "2048"}})
	}
	return entries, nil
}
