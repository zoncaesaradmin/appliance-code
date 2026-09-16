# Private AI runtime and model management

Private AI is a first-class appliance capability. It is not implemented through
the generic Applications catalog because the platform owns its authentication,
stable API, persistent model storage, lifecycle checks, and upgrade contract.

## Runtime packages

Package metadata carries only the minimum runtime contract:

| Package | Engine | Architecture | Acceleration |
| --- | --- | --- | --- |
| `std-llm-amd64` | Ollama | `amd64` | standard |
| `acc-llm-amd64` | vLLM | `amd64` | accelerated (GPU required) |
| `acc-llm-arm64` | vLLM | `arm64` | accelerated (GPU required) |

These entries are not a supported-model or hardware-vendor catalog. The package
declares `{inferenceEngine, architecture}` only. Runtime capability checks report
the host architecture, acceleration class (`standard` or `accelerated`), and
optional `gpuAvailable`. There is no product-level `supportedModes`,
`inferenceMode`, or CPU/CUDA mode switch.

- **Standard** (`std-llm-*`): Ollama. Install does not require a GPU. At runtime
  the manager may use a host GPU when one is present; otherwise it serves on CPU.
- **Accelerated** (`acc-llm-*`): vLLM. Install fails closed without a usable
  NVIDIA GPU on the host. `gpu.enabled` in Helm values follows that host check.

The control-plane API exposes `RuntimeCapabilities` with `package`, `engine`,
`architecture`, `hostArchitecture`, `acceleration`, optional `gpuAvailable`, and
`checks`. It does not expose `supportedModes`, `requestedMode`, or `activeMode`.

More architecture-specific packages can be added later without changing the
profile or public API.

The signed package still contains the pinned inference engine image and chart.
It does not contain model weights. The `inference` capability is enabled by a
profile, while the release index and host architecture select the one package
that supplies it. Merely including a package never enables the capability.

## Public and administrative APIs

The public OpenAI-compatible prefix is `/inference/{path...}`. The control plane
authenticates and authorizes the request, strips `/inference`, and streams the
remainder to the inference manager `/v1/*`, which proxies to the selected
engine. Before proxying `POST /v1/responses`, the manager normalizes OpenAI
`text.format.type=json_schema` so packaged runtimes can stream safely. On GPU,
schema-only requests remap to constrained generation (`structured_outputs.json`)
or OpenAI JSON mode. On CPU, structured_outputs is never forwarded: vLLM's
xgrammar bitmask path calls `pin_memory` and fatally kills EngineCore
("pin_memory=True requires a CUDA or other accelerator backend"). CPU requests
keep schema guidance in instructions only. When tools are present (typical for
coding agents on the Responses wire API), the broken `json_schema` format is
stripped and the schema is preserved only as instruction guidance so tool-call
tokens remain expressible. That keeps Responses streaming healthy for Codex
(`wire_api = "responses"`), Continue, custom SDKs, and similar OpenAI-compatible
clients. Engine-native management endpoints are never exposed under this prefix.
(Legacy `/ai/v1/*` remains as an alias where still configured.)

Administrators use a separate engine-neutral lifecycle API on the control plane,
which calls manager-owned `/internal/v1/...` routes (not the OpenAI proxy):

- `GET /api/v1/inference/runtime-capabilities`
- `GET /api/v1/inference/status`
- `GET /api/v1/inference/models` → manager `GET /internal/v1/models` (downloaded inventory)
- `GET /api/v1/inference/models/catalog`
- `POST /api/v1/inference/models/imports`
- `GET /api/v1/inference/models/imports/progress`
- `POST /api/v1/inference/models/load`
- `GET /api/v1/inference/models/load/progress`
- `POST /api/v1/inference/models/delete`

Reads require `inference.models.read`, mutations require `inference.admin`, and
OpenAI inference calls require `inference.use`. Mutations are audited. The first
implementation serializes model mutations and returns `409` if another one is
running. This is intentional: downloads can be large, and concurrent imports
would make storage and memory admission unpredictable. A later job-backed
implementation may queue multiple requests without changing these resources.

Every inference package deploys a steady-state **inference-manager** Deployment
only. On Load, the manager applies an on-demand **inference-engine** Deployment
(docker-run semantics via Kubernetes) with model-derived memory limits and the
full serve argv, then blind-proxies `/v1/*` to the engine Service. When no model
is loaded there is no engine pod; OpenAI routes return `503`. Admin
`/internal/v1` stays on the manager. The Ollama adapter maps to pull, tags,
generate/keep-alive, and delete against a transient engine Deployment. The vLLM
adapter downloads and verifies a source, records installed models, and
creates/replaces the single active engine Deployment when `load` is requested.
Multiple models may be stored, but one engine base model is active at a time.

vLLM launch configuration is stored with the imported model as a validated
argv array—never shell text. The current contract accepts quantization,
maximum model length, GPU-memory utilization, CUDA graph capture size,
FlashInfer autotune disablement, automatic tool choice, served model name, and
tool-call parser. `--max-model-len`, engine memory, and engine CPU are planned
together with catalog eligibility from the model card, remaining host/GPU
memory for KV, host CPU capacity, and the device prefill ceiling (CPU uses a few
chunked-prefill steps so interactive agents stay responsive). Load rewrites
legacy fixed-2048 caps and oversized card-only windows to that planned value
and persists it for client copy settings. The manager owns the
model path, bind address, and port so
callers cannot bypass the appliance boundary. Device selection is owned by the
manager (`resolveDevice` / `gpuProbe` / internal `gpu|cpu` labels); charts do
not expose `runtime.mode` or `supportedModes`.

The tested Docker invocation maps to the on-demand engine pod without rebuilding
the vLLM environment:

| Docker setting | Appliance pod equivalent |
| --- | --- |
| official `vllm/vllm-openai` image | pinned `registry.local/inference-runtime@sha256:…` |
| `docker run … --model …` | engine Deployment `command`/`args` set on Load |
| `--gpus all` | `runtimeClassName: nvidia` plus `NVIDIA_VISIBLE_DEVICES=all` when GPU enabled |
| `--ipc=host` | isolated, memory-backed `/dev/shm` emptyDir (not host IPC) |
| memlock/stack ulimits | inherited container defaults under Restricted PSA |
| port `8000`/`8001` | in-namespace `inference-engine` Service; manager proxies `/v1/*` |
| Hugging Face and vLLM caches | persistent model PVC cache directories |
| vLLM server flags | validated `launchArguments` argv stored with the model |
| cgroup memory | model `memoryBytes` + overhead, clamped by package `engine.maxMemory` |

Host IPC is intentionally not enabled: the isolated `/dev/shm` mount supplies
the shared memory vLLM needs without weakening Restricted pod isolation.

## Connected model-acquisition window

Installation, startup, inference using already installed models, and all other
normal appliance operation remain offline. Model acquisition is the narrow
exception: after installation an administrator may connect the appliance,
explicitly request the desired models, wait for verification and installation,
and disconnect it again. The inference NetworkPolicy permits DNS plus outbound
HTTPS for that administrator-directed fetch. It does not download engines,
drivers, plugins, updates, or models in the background.

The admin UI is available at **Admin → AI Services**. It shows runtime readiness,
acceleration class, and optional GPU availability, lists installed models,
accepts an engine-supported model reference, and provides load and remove
actions.

## Accelerated package completion gate

`acc-llm-amd64` and `acc-llm-arm64` package pinned vLLM images with the appliance
runtime manager. They require a usable GPU at install time and again when the
manager confirms `gpuAvailable`. They can start without a model, download an
explicitly requested Hugging Face snapshot, verify its deterministic content
digest, and start one selected model behind the stable OpenAI proxy.

`acc-llm-arm64` is selected explicitly; it is not included by `all` while we
maintain the standard AMD64 delivery baseline. Each release must validate OpenAI
streaming, model switching, persistence, rollback, backup/restore, and
interrupted-download cleanup for the exact image digest it publishes.
