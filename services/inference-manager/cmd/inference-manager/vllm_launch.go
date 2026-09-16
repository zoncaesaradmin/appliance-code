package main

import (
	"encoding/json"
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
	return modelArchFromConfigJSON(data).MaxPosition
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
	return modelArchFromModelDir(modelDir).MaxPosition
}

func findMaxModelLenIndex(arguments []string) int {
	for index := 0; index+1 < len(arguments); index++ {
		if strings.TrimSpace(arguments[index]) == "--max-model-len" {
			return index
		}
	}
	return -1
}

func maxModelLenFromArgs(arguments []string) uint64 {
	if index := findMaxModelLenIndex(arguments); index >= 0 {
		if parsed, err := strconv.ParseUint(strings.TrimSpace(arguments[index+1]), 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func setMaxModelLenArg(arguments []string, maxLen uint64) []string {
	if maxLen == 0 {
		return append([]string(nil), arguments...)
	}
	out := append([]string(nil), arguments...)
	value := strconv.FormatUint(maxLen, 10)
	if index := findMaxModelLenIndex(out); index >= 0 {
		out[index+1] = value
		return out
	}
	return append(out, "--max-model-len", value)
}

// applyServeWindowMaxModelLen writes the planned window into launch args.
// An explicit operator-chosen value below the plan is kept; values above the
// plan, missing values, zeros, and legacy 2048 caps are replaced by the plan.
func applyServeWindowMaxModelLen(arguments []string, planned uint64, modelCardLimit uint64) []string {
	if planned == 0 {
		return append([]string(nil), arguments...)
	}
	out := append([]string(nil), arguments...)
	index := findMaxModelLenIndex(out)
	if index < 0 {
		return setMaxModelLenArg(out, planned)
	}
	requested, err := strconv.ParseUint(strings.TrimSpace(out[index+1]), 10, 64)
	if err != nil || requested == 0 || requested > planned || isLegacyCappedMaxModelLen(requested, modelCardLimit) {
		out[index+1] = strconv.FormatUint(planned, 10)
		return out
	}
	return out
}

// clampLaunchMaxModelLen sets --max-model-len from the installed model's
// config.json when available, then applies hardware/mode planning when mode
// and budget are supplied via planServe at Load time.
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
// promote those installs to the planned serve window.
func isLegacyCappedMaxModelLen(requested, modelLimit uint64) bool {
	return requested == 2048 && modelLimit > 2048
}

func effectiveMaxModelLen(arguments []string, modelDir string) uint64 {
	clamped := clampLaunchMaxModelLen(arguments, modelDir)
	if parsed := maxModelLenFromArgs(clamped); parsed > 0 {
		return parsed
	}
	return maxPositionFromModelDir(modelDir)
}
