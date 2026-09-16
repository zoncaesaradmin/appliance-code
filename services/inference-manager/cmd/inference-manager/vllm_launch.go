package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const preferredMaxModelLen = 2048

func resolveMaxModelLen(modelLimit uint64) uint64 {
	if modelLimit == 0 {
		return preferredMaxModelLen
	}
	if modelLimit < preferredMaxModelLen {
		return modelLimit
	}
	return preferredMaxModelLen
}

func launchArgsForCatalog(modelLimit uint64) []string {
	return []string{"--max-model-len", strconv.FormatUint(resolveMaxModelLen(modelLimit), 10)}
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

// clampLaunchMaxModelLen ensures --max-model-len never exceeds the model's
// configured context window (vLLM rejects oversized values at startup).
// When the model window is unknown, existing arguments are left unchanged.
func clampLaunchMaxModelLen(arguments []string, modelDir string) []string {
	modelLimit := maxPositionFromModelDir(modelDir)
	out := append([]string(nil), arguments...)
	hasMaxLen := false
	for index := 0; index+1 < len(out); index++ {
		if strings.TrimSpace(out[index]) != "--max-model-len" {
			continue
		}
		hasMaxLen = true
		if modelLimit == 0 {
			return out
		}
		limit := resolveMaxModelLen(modelLimit)
		requested, err := strconv.ParseUint(strings.TrimSpace(out[index+1]), 10, 64)
		if err != nil || requested == 0 || requested > limit {
			out[index+1] = strconv.FormatUint(limit, 10)
		}
		return out
	}
	if hasMaxLen {
		return out
	}
	return append(out, "--max-model-len", strconv.FormatUint(resolveMaxModelLen(modelLimit), 10))
}

type engineExitStatus struct {
	Generation string `json:"generation"`
	ExitCode   int    `json:"exitCode"`
}

func (m *manager) clearEngineExitStatus() {
	if m.controlDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(m.controlDir, "engine-exit.json"))
}

func (m *manager) readEngineExitStatus() (engineExitStatus, bool) {
	if m.controlDir == "" {
		return engineExitStatus{}, false
	}
	data, err := os.ReadFile(filepath.Join(m.controlDir, "engine-exit.json"))
	if err != nil {
		return engineExitStatus{}, false
	}
	var status engineExitStatus
	if err := json.Unmarshal(data, &status); err != nil || status.Generation == "" {
		return engineExitStatus{}, false
	}
	return status, true
}

func (m *manager) engineExitedForCurrentGeneration() (engineExitStatus, bool) {
	m.processMu.Lock()
	generation := m.generation
	m.processMu.Unlock()
	if generation == "" {
		return engineExitStatus{}, false
	}
	status, ok := m.readEngineExitStatus()
	if !ok || status.Generation != generation {
		return engineExitStatus{}, false
	}
	return status, true
}
