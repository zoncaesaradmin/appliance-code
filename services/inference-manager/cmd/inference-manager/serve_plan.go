package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Chunked prefill token budget matches packaged vLLM CPU defaults
// (logged as max_num_batched_tokens=2048 on v0.17.1).
const chunkedPrefillTokens = 2048

// serveWindowInput is the single input set used to decide catalog eligibility,
// engine memory/CPU limits, and --max-model-len together.
type serveWindowInput struct {
	Engine             string
	Mode               string // cpu|cuda|""
	ModelEstimateBytes uint64
	AvailableBytes     uint64
	AvailableCPUs      uint64 // 0 → detect from host
	ModelContextLimit  uint64 // model card max_position / n_positions
	KVBytesPerToken    uint64 // 0 → heuristic fallback from model size
}

// servePlan is the one planning result shared by catalog and Load.
type servePlan struct {
	MaxModelLen        uint64
	ModelEstimateBytes uint64
	KVBytes            uint64
	ShmBytes           uint64
	PodMarginBytes     uint64
	RequiredBytes      uint64
	AvailableBytes     uint64
	KVBytesPerToken    uint64
	CPUCores           uint64
	CPULimit           resource.Quantity
	CPURequest         resource.Quantity
}

type modelArch struct {
	MaxPosition uint64
	Layers      uint64
	KVHeads     uint64
	HeadDim     uint64
}

func (a modelArch) kvBytesPerToken() uint64 {
	// K+V, bf16/fp16 (2 bytes): 2 * layers * kv_heads * head_dim * 2
	if a.Layers == 0 || a.KVHeads == 0 || a.HeadDim == 0 {
		return 0
	}
	return 2 * a.Layers * a.KVHeads * a.HeadDim * 2
}

func modelArchFromConfigJSON(data []byte) modelArch {
	var config struct {
		MaxPositionEmbeddings json.Number `json:"max_position_embeddings"`
		NPositions            json.Number `json:"n_positions"`
		NumHiddenLayers       json.Number `json:"num_hidden_layers"`
		NLayer                json.Number `json:"n_layer"`
		NumKeyValueHeads      json.Number `json:"num_key_value_heads"`
		NumAttentionHeads     json.Number `json:"num_attention_heads"`
		NHead                 json.Number `json:"n_head"`
		HiddenSize            json.Number `json:"hidden_size"`
		NEmbd                 json.Number `json:"n_embd"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return modelArch{}
	}
	arch := modelArch{
		MaxPosition: maxPositionFromNumbers(config.MaxPositionEmbeddings, config.NPositions),
		Layers:      uintFromNumbers(config.NumHiddenLayers, config.NLayer),
		KVHeads:     uintFromNumbers(config.NumKeyValueHeads),
	}
	heads := uintFromNumbers(config.NumAttentionHeads, config.NHead)
	hidden := uintFromNumbers(config.HiddenSize, config.NEmbd)
	if arch.KVHeads == 0 {
		arch.KVHeads = heads
	}
	if heads > 0 && hidden > 0 && hidden%heads == 0 {
		arch.HeadDim = hidden / heads
	}
	return arch
}

func modelArchFromModelDir(modelDir string) modelArch {
	data, err := os.ReadFile(filepath.Join(modelDir, "config.json"))
	if err != nil {
		return modelArch{}
	}
	return modelArchFromConfigJSON(data)
}

func uintFromNumbers(values ...json.Number) uint64 {
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

func fallbackKVBytesPerToken(modelEstimateBytes uint64) uint64 {
	// When architecture fields are missing, use a conservative mid-size
	// transformer stand-in (~32 KiB/token). Observed Qwen2.5-3B is ~36 KiB.
	_ = modelEstimateBytes
	return 32 << 10
}

// modePrefillContextCap derives a practical context ceiling from the engine's
// chunked-prefill step size and how many steps a mode can absorb before
// interactive clients stall with no tokens.
func modePrefillContextCap(mode string) uint64 {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "gpu", "cuda": // cuda kept as alias for older test fixtures during transition
		return chunkedPrefillTokens * 64 // 131072; memory/KV usually dominates
	case "cpu", "":
		// 4 × 2048 = 8192: enough for agent prompts without multi-minute first token.
		return chunkedPrefillTokens * 4
	default:
		return chunkedPrefillTokens * 4
	}
}

func minPositiveUint64(values ...uint64) uint64 {
	var best uint64
	have := false
	for _, value := range values {
		if value == 0 {
			continue
		}
		if !have || value < best {
			best = value
			have = true
		}
	}
	if !have {
		return 0
	}
	return best
}

func hostCPUCount() uint64 {
	f, err := os.Open("/proc/cpuinfo")
	if err == nil {
		defer f.Close()
		var count uint64
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "processor") {
				count++
			}
		}
		if count > 0 {
			return count
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return uint64(n)
}

// planCPUCores chooses engine CPU from host capacity and mode.
// Optional INFERENCE_ENGINE_CPU_LIMIT / _REQUEST override when set (lab pin).
func planCPUCores(mode string, availableCPUs uint64) (cores uint64, limit, request resource.Quantity) {
	if raw := strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_CPU_LIMIT")); raw != "" {
		limit = parseQuantity(raw, "4")
		reqRaw := strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_CPU_REQUEST"))
		if reqRaw == "" {
			reqRaw = "100m"
		}
		request = parseQuantity(reqRaw, "100m")
		if milli := limit.MilliValue(); milli > 0 {
			cores = uint64((milli + 999) / 1000)
			if cores < 1 {
				cores = 1
			}
		}
		return cores, limit, request
	}

	if availableCPUs == 0 {
		availableCPUs = hostCPUCount()
	}
	if availableCPUs < 1 {
		availableCPUs = 1
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "gpu", "cuda":
		// GPU does the heavy math; keep a modest host CPU reservation.
		cores = availableCPUs / 4
		if cores < 2 {
			cores = 2
		}
		if cores > 4 {
			cores = 4
		}
		if cores > availableCPUs {
			cores = availableCPUs
		}
		limit = *resource.NewQuantity(int64(cores), resource.DecimalSI)
		request = *resource.NewMilliQuantity(int64(cores)*500, resource.DecimalSI)
	default:
		// CPU inference: give most host cores to the engine (leave ~25% for
		// control-plane / OS). Match OMP thread budget to the cgroup limit so
		// vLLM does not bind more threads than CFS will schedule.
		cores = availableCPUs * 3 / 4
		if cores < 2 && availableCPUs >= 2 {
			cores = 2
		}
		if cores < 1 {
			cores = 1
		}
		if cores > availableCPUs {
			cores = availableCPUs
		}
		limit = *resource.NewQuantity(int64(cores), resource.DecimalSI)
		request = *resource.NewQuantity(int64(cores), resource.DecimalSI)
	}
	return cores, limit, request
}

// planServe is the single planner for catalog eligibility, Load resources
// (memory + CPU), and --max-model-len. requiredBytes includes KV for the
// planned window.
func planServe(in serveWindowInput) (servePlan, error) {
	plan := servePlan{
		ModelEstimateBytes: in.ModelEstimateBytes,
		ShmBytes:           configuredShmBytes(in.Engine),
		PodMarginBytes:     podMarginBytes,
		AvailableBytes:     in.AvailableBytes,
		KVBytesPerToken:    in.KVBytesPerToken,
	}
	plan.CPUCores, plan.CPULimit, plan.CPURequest = planCPUCores(in.Mode, in.AvailableCPUs)
	if plan.KVBytesPerToken == 0 {
		plan.KVBytesPerToken = fallbackKVBytesPerToken(in.ModelEstimateBytes)
	}
	if in.ModelEstimateBytes == 0 {
		return plan, errors.New("model memory estimate is unavailable")
	}

	reserved := in.ModelEstimateBytes + plan.ShmBytes + plan.PodMarginBytes
	var memCap uint64 = math.MaxUint64
	if in.AvailableBytes > 0 {
		if in.AvailableBytes <= reserved {
			plan.RequiredBytes = reserved
			return plan, fmt.Errorf("model needs %d bytes (estimate %d + shm %d + margin %d); only %d bytes available",
				reserved, in.ModelEstimateBytes, plan.ShmBytes, plan.PodMarginBytes, in.AvailableBytes)
		}
		kvBudget := (in.AvailableBytes - reserved) / 2
		if plan.KVBytesPerToken > 0 {
			memCap = kvBudget / plan.KVBytesPerToken
		}
	}

	card := in.ModelContextLimit
	modeCap := modePrefillContextCap(in.Mode)
	maxLen := minPositiveUint64(card, memCap, modeCap)
	if maxLen == 0 {
		maxLen = minPositiveUint64(memCap, modeCap)
	}

	for maxLen > 0 {
		kv := maxLen * plan.KVBytesPerToken
		required := reserved + kv
		plan.MaxModelLen = maxLen
		plan.KVBytes = kv
		plan.RequiredBytes = required
		if in.AvailableBytes == 0 || required <= in.AvailableBytes {
			if max := packageMaxMemoryBytes(); max > 0 && required > max {
				overflow := required - max
				shrink := (overflow + plan.KVBytesPerToken - 1) / plan.KVBytesPerToken
				if shrink >= maxLen {
					return plan, fmt.Errorf("model needs %d bytes; configured package max is %d bytes", required, max)
				}
				maxLen -= shrink
				continue
			}
			return plan, nil
		}
		overflow := required - in.AvailableBytes
		shrink := (overflow + plan.KVBytesPerToken - 1) / plan.KVBytesPerToken
		if shrink >= maxLen {
			break
		}
		maxLen -= shrink
	}

	plan.RequiredBytes = reserved + plan.KVBytesPerToken
	return plan, fmt.Errorf("model needs %d bytes with a usable context window; only %d bytes available",
		reserved+plan.KVBytesPerToken, in.AvailableBytes)
}

// planModelMemory remains the weight+shm+margin helper for ollama / idle paths.
// vLLM catalog eligibility and Load must use planServe so KV for the planned
// context window is included in requiredBytes.
func planModelMemory(engine string, modelEstimateBytes, availableBytes uint64) (memoryPlan, error) {
	plan := memoryPlan{
		ModelEstimateBytes: modelEstimateBytes,
		ShmBytes:           configuredShmBytes(engine),
		PodMarginBytes:     podMarginBytes,
		AvailableBytes:     availableBytes,
	}
	if modelEstimateBytes == 0 {
		return plan, errors.New("model memory estimate is unavailable")
	}
	plan.RequiredBytes = modelEstimateBytes + plan.ShmBytes + plan.PodMarginBytes
	if availableBytes > 0 && plan.RequiredBytes > availableBytes {
		return plan, fmt.Errorf("model needs %d bytes (estimate %d + shm %d + margin %d); only %d bytes available",
			plan.RequiredBytes, modelEstimateBytes, plan.ShmBytes, plan.PodMarginBytes, availableBytes)
	}
	if max := packageMaxMemoryBytes(); max > 0 && plan.RequiredBytes > max {
		return plan, fmt.Errorf("model needs %d bytes; configured package max is %d bytes", plan.RequiredBytes, max)
	}
	return plan, nil
}

func engineResourceSpecFromPlan(plan servePlan) (enginePodSpec, error) {
	if plan.RequiredBytes == 0 {
		return enginePodSpec{}, errors.New("model memory estimate is unavailable")
	}
	limit := *resource.NewQuantity(int64(plan.RequiredBytes), resource.BinarySI)
	req := *resource.NewQuantity(limit.Value()/2, resource.BinarySI)
	if req.Cmp(resource.MustParse("512Mi")) < 0 {
		req = resource.MustParse("512Mi")
	}
	return enginePodSpec{
		MemoryLimit: limit,
		MemoryReq:   req,
		CPULimit:    plan.CPULimit,
		CPURequest:  plan.CPURequest,
		OMPThreads:  int(plan.CPUCores),
		GPURequest:  strings.EqualFold(env("INFERENCE_GPU_ENABLED", "false"), "true"),
	}, nil
}
