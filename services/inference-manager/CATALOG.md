# Model discovery

The manager refreshes metadata on first start and every 24 hours thereafter.
It persists the last attempt, last successful catalog, and error under
`/models/.appliance-catalog/<engine>.json` on the existing model PVC. A restart
uses the saved schedule. Failed or empty refreshes retain the last good list;
failure never prevents startup or use of downloaded models. No model weights
are downloaded by this job. Operators control connectivity at the network edge.

Discovery is bounded to popular upstream candidates: up to 24 Ollama families
and eight explicit tags each, or 40 Hugging Face text-generation repositories.
The limits avoid mirroring huge registries daily. This is not an exhaustive
supported-model matrix. Ollama's HTML library adapter fails closed if discovery
breaks; its registry manifests provide actual layer sizes. Hugging Face uses
repository metadata with pinned commit revisions. vLLM candidates must match
architectures declared in the installed ModelRegistry source (parsed without
importing the vLLM runtime, which is unsafe in the Restricted CPU pod) and use
non-quantized safetensors with no custom-code configuration; gated/private
repositories are excluded. Advanced quantized models remain available through
the existing explicit import API.

Eligibility is an estimate, not a guarantee of runtime compatibility, especially
for new model families in older Ollama versions. The UI states that a load is
required to verify execution. No hardware vendors or specific model names are
hardcoded. Runtime mode, host available memory, optional package maxMemory,
GPU free memory for CUDA, and PVC free space determine current eligibility.
GPU memory is not summed across devices; automatic tensor parallelism is not
configured. Conservatively reserve 25% of CPU memory (20% of GPU memory). Model
`memoryBytes` uses twice the weights plus 2 GiB (Ollama) or 4 GiB (vLLM). For
vLLM, one planner (`planServe`) decides catalog eligibility, engine memory and
CPU limits, and `--max-model-len` together:

- model card window (`max_position_embeddings` / `n_positions`)
- KV bytes that fit in remaining host/GPU memory (architecture-derived
  bytes/token when config fields are known)
- mode prefill ceiling from the engine chunked-prefill step (CPU:
  `4 × 2048 = 8192`; CUDA: memory usually dominates)
- CPU cores from host capacity (CPU mode ≈ 75% of host CPUs with matching
  OMP thread budget; CUDA keeps a modest host CPU reservation)

`requiredBytes` therefore includes KV for that planned window, plus vLLM
`/dev/shm` and a 512 MiB pod margin, so the catalog “load needs” figure matches
the engine Deployment memory limit. Load persists the effective launch
arguments so client copy settings match the running engine.

The packaged CPU image may log a benign `vllm._C_AVX2` import warning; upstream
documents that the AVX2 library still loads. Hosts with AVX2 are expected to
use those kernels.

A successful catalog (or a retained non-empty one) refreshes at most once per day
so restarts do not hammer upstream. If discovery has never succeeded and the
cached list is empty, the next process start retries immediately once so a fixed
runtime image is not blocked for 24 hours by a prior failed attempt. After that
retry, failures return to the daily schedule.
That immediate retry is allowed only once per process start. If it fails again,
the process returns to the daily schedule; loss of connectivity cannot create
a tight retry loop.

## Registry and offline boundaries

The upstream model catalog is independent of the appliance OCI registry.
`registry.ollama.ai` provides model manifests, while Hugging Face provides model
metadata and snapshots. Neither is used to fetch runtime container images.
Runtime images continue through the existing seeded LAN/online build policy,
signed bundle, digest verification, and K3s preload path. Catalog metadata and
downloaded weights persist on the inference PVC, not the OCI registry.

The vLLM serving engine Deployment uses local model paths with `HF_HUB_OFFLINE=1` and
`TRANSFORMERS_OFFLINE=1`; usage reporting is disabled with
`HF_HUB_DISABLE_TELEMETRY=1`, `VLLM_NO_USAGE_STATS=1`, and `DO_NOT_TRACK=1`.
These overrides apply only to the engine pod, preserving upstream catalog refreshes
and administrator-triggered model downloads in the manager. An offline refresh
failure retains the previous catalog and never removes installed models.

`GET /internal/v1/models/catalog` returns cached candidates and eligibility.
`GET /internal/v1/models` returns the manager's downloaded inventory (admin).
`GET /v1/models` (and the rest of `/v1/*`) is proxied to the inference
engine for OpenAI-compatible clients. For `POST /v1/responses`, the manager
first rewrites OpenAI `text.format.type=json_schema` so streaming clients do
not hit the known `response.created` schema alias crash on older CPU runtime
images. CUDA may use constrained generation (`structured_outputs`); CPU never
does — xgrammar's `pin_memory` path crashes EngineCore on CPU-only builds, so
schema guidance stays in instructions only. Requests that also carry tools keep
tool calling and move schema guidance into instructions. Control-plane admin
APIs use the `/internal/v1/...` surface only:

| Control plane | Inference manager |
|---|---|
| `GET /api/v1/inference/models` | `GET /internal/v1/models` |
| `GET /api/v1/inference/models/catalog` | `GET /internal/v1/models/catalog` |
| `POST /api/v1/inference/models/imports` | `POST /internal/v1/models/imports` |
| `GET /api/v1/inference/models/imports/progress` | `GET /internal/v1/models/imports/progress` |
| `POST /api/v1/inference/models/load` | `POST /internal/v1/models/load` |
| `GET /api/v1/inference/models/load/progress` | `GET /internal/v1/models/load/progress` |
| `POST /api/v1/inference/models/delete` | `POST /internal/v1/models/delete` |
| `GET /api/v1/inference/runtime-capabilities` | `GET /internal/v1/runtime/capabilities` |
| External `/inference/{path...}` (strip `/inference`) | `/v1/*` → engine proxy |

Catalog imports include `catalogId` alongside matching `modelId` and `source`;
the manager revalidates capacity and takes launch arguments from the cache.
Explicit imports retain the existing administrator API for advanced uses.

Unit tests exercise the real `discoverVLLM` / `discoverOllama` path against a
local HTTPS fixture (no public network required) and prove architecture probing
parses the installed registry without importing the vLLM runtime. An install-time
Job publishes `/models/.zon/vllm-architectures.json` for the thin manager; the
manager falls back to a local registry probe only when that file is unavailable
(tests and legacy images).

Eligibility and Load share one vLLM serve plan (`planServe`):

```
MaxModelLen = min(model card, KV memory cap, mode prefill cap)
requiredBytes = memoryBytes + shmBytes + 512Mi margin + KVBytesPerToken×MaxModelLen
cpuCores = ~75% host CPUs (CPU mode) or modest reservation (CUDA)
availableBytes = host MemAvailable × 0.75 (or GPU free × 0.8 in CUDA mode
when the manager can probe GPU free memory via torch or nvidia-smi; the thin
manager pod often cannot, so it keeps the host MemAvailable fallback instead of
treating a probe miss as zero capacity)
eligible <=> requiredBytes <= availableBytes
engine pod memory limit = requiredBytes
engine pod CPU limit/request = cpuCores (OMP_NUM_THREADS matched)
--max-model-len = MaxModelLen (persisted on Load)
CUDA launch defaults: --gpu-memory-utilization 0.8 unless already set
(INFERENCE_VLLM_GPU_MEMORY_UTILIZATION / explicit launch arg). Engine GPU access
uses RuntimeClass + NVIDIA_VISIBLE_DEVICES; nvidia.com/gpu is opt-in via
INFERENCE_GPU_RESOURCE_REQUEST (device plugin required).
```

`memoryBytes` is the catalog model estimate (weights×2 plus engine headroom
from discovery). Catalog responses also include `requiredBytes`,
`modelContextLimit`, and `availableMemoryBytes`. Optional chart pins
(`engine.maxMemory`, `engine.cpuLimit`) override planning only when set; the
defaults are empty (host-derived).

Model architecture parsing prefers an explicit `head_dim` from `config.json`
(required for Qwen3 where `head_dim` ≠ `hidden_size / num_attention_heads`).

UI download state is derived from the runtime inventory, never inferred from
catalog membership. Removing or losing a catalog entry does not remove its
download or prevent loading/deleting it. Offline catalog status includes the
last successful refresh and failure so cached metadata is not presented as fresh.
