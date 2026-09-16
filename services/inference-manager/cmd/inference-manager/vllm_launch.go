package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func launchArgsForCatalog(modelLimit uint64) []string {
	if modelLimit == 0 {
		return nil
	}
	return []string{"--max-model-len", strconv.FormatUint(modelLimit, 10)}
}

func maxPositionFromConfigJSON(data []byte) uint64 {
	var config struct {
		MaxPositionEmbeddings json.Number `json:"max_position_embeddings"`
		NPositions            json.Number `json:"n_positions"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return 0
	}
	return maxPositionFromNumbers(config.MaxPositionEmbeddings, config.NPositions)
}

func maxPositionFromNumbers(values ...json.Number) uint64 {
	for _, value := range values {
		if value == "" {
			continue
		}
		if parsed, err := value.Float64(); err == nil && parsed > 0 {
			return uint64(parsed)
		}
	}
	return 0
}

func maxPositionFromModelDir(modelDir string) uint64 {
	data, err := os.ReadFile(filepath.Join(modelDir, "config.json"))
	if err != nil {
		return 0
	}
	return maxPositionFromConfigJSON(data)
}

func findMaxModelLenIndex(arguments []string) int {
	for index := 0; index+1 < len(arguments); index++ {
		if strings.TrimSpace(arguments[index]) == "--max-model-len" {
			return index
		}
	}
	return -1
}

// clampLaunchMaxModelLen sets --max-model-len from the installed model's
// config.json when available. That value is authoritative for Load: catalog
// estimates and older capped defaults must not keep the serve window at 2048
// when the model card says otherwise. Requests above the model window are
// clamped down (vLLM rejects oversized values).
func clampLaunchMaxModelLen(arguments []string, modelDir string) []string {
	modelLimit := maxPositionFromModelDir(modelDir)
	out := append([]string(nil), arguments...)
	index := findMaxModelLenIndex(out)

	if modelLimit > 0 {
		value := strconv.FormatUint(modelLimit, 10)
		if index >= 0 {
			requested, err := strconv.ParseUint(strings.TrimSpace(out[index+1]), 10, 64)
			// Keep an explicit operator-chosen lower window; raise/replace
			// missing, zero, oversized, or legacy-capped values with the model window.
			if err == nil && requested > 0 && requested < modelLimit && !isLegacyCappedMaxModelLen(requested, modelLimit) {
				return out
			}
			out[index+1] = value
			return out
		}
		return append(out, "--max-model-len", value)
	}

	if index >= 0 {
		return out
	}
	return out
}

// isLegacyCappedMaxModelLen detects the previous product default that forced
// --max-model-len 2048 even when the model window was larger. Reloading must
// promote those installs to the real model window.
func isLegacyCappedMaxModelLen(requested, modelLimit uint64) bool {
	return requested == 2048 && modelLimit > 2048
}

func effectiveMaxModelLen(arguments []string, modelDir string) uint64 {
	clamped := clampLaunchMaxModelLen(arguments, modelDir)
	if index := findMaxModelLenIndex(clamped); index >= 0 {
		if parsed, err := strconv.ParseUint(strings.TrimSpace(clamped[index+1]), 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return maxPositionFromModelDir(modelDir)
}
