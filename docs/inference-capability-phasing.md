# Private AI runtime and model management

Private AI is a first-class appliance capability. It is not implemented through
the generic Applications catalog because the platform owns its authentication,
stable API, persistent model storage, lifecycle checks, and upgrade contract.

## Runtime packages

Package metadata carries only the minimum runtime contract:

| Package | Engine | Architecture | Package modes |
| --- | --- | --- | --- |
| `std-llm-amd64` | Ollama | `amd64` | `cpu` |
| `acc-llm-amd64` | vLLM | `amd64` | `cpu` |
| `acc-llm-arm64` | vLLM | `arm64` | `cpu`, `cuda` |

These entries are not a supported-model or hardware-vendor catalog. The package
declares modes its runtime can implement; runtime capability checks report the
host architecture and the actually selected mode. Installation requests `auto`.
The manager prefers CUDA when the signed package allows it and its own CUDA
backend can use a visible device; otherwise it selects CPU. It never infers
CUDA from ARM, a machine name, or a vendor string. The API reports both
`availableModes` and `activeMode`, including why a CUDA probe was rejected.

For a future CUDA-capable package, install-time hardware discovery must first
confirm that Kubernetes can advertise a GPU resource and configure the pod to
request it. The manager then performs the final in-container CUDA probe before
loading a model. If either check fails, the deployment uses CPU. There is no
normal user-facing mode picker; an explicit non-`auto` mode is reserved for a
future controlled diagnostic or policy override.
More architecture-specific packages can be added later without changing the
profile or public API.

The signed package still contains the pinned inference engine image and chart.
It does not contain model weights. The `inference` capability is enabled by a
profile, while the release index and host architecture select the one package
that supplies it. Merely including a package never enables the capability.

## Public and administrative APIs

The public OpenAI-compatible prefix is `/inference/{path...}`. The control plane
authenticates and authorizes the request, strips `/inference`, and streams the
remainder to the inference manager `/v1/*`, which blind-proxies to the selected
engine. Engine-native management endpoints are never exposed under this prefix.
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

Every inference image now uses the appliance manager as its entrypoint. The
manager starts the packaged engine process, owns health plus `/internal/v1`
admin lifecycle endpoints, and blind-proxies only `/v1/*` to the engine. The
Ollama adapter maps to pull, tags, generate/keep-alive, and delete. The vLLM
adapter downloads and verifies a source, records installed models, and
restarts/switches the single active vLLM server when `load` is requested.
Multiple models may be stored, but one vLLM base model is active per runtime pod.

vLLM launch configuration is stored with the imported model as a validated
argv array—never shell text. The current contract accepts quantization,
maximum model length, GPU-memory utilization, CUDA graph capture size,
FlashInfer autotune disablement, automatic tool choice, served model name, and
tool-call parser. The manager owns the model path, bind address, and port so
callers cannot bypass the appliance boundary. Device selection is owned by the
packaged runtime image and mode detection (CPU vs CUDA build / visible GPU),
not by a `--device` CLI flag—current vLLM CPU images reject `--device`.

The tested Docker invocation maps to the pod without rebuilding the vLLM
environment:

| Docker setting | Appliance pod equivalent |
| --- | --- |
| official `vllm/vllm-openai` image | pinned base of the managed inference image |
| `--gpus all` | detected K3s `nvidia` RuntimeClass plus `NVIDIA_VISIBLE_DEVICES=all` |
| `--ipc=host` | isolated, memory-backed `/dev/shm` volume |
| memlock/stack ulimits | manager raises inherited memlock to the container maximum and requests a 64 MiB stack |
| port `8000` | manager-only loopback backend, exposed through the appliance Service on `/ai/v1` |
| Hugging Face and vLLM caches | persistent model PVC cache directories |
| vLLM server flags | validated `launchArguments` argv stored with the model |

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

The admin UI is available at **Admin → AI Services**. It shows runtime readiness
and detected mode, lists installed models, accepts an engine-supported model
reference, and provides load and remove actions.

## Accelerated package completion gate

`acc-llm-amd64` packages the pinned x86 CPU vLLM image with the appliance
runtime manager. It can start without a model, validates the host's minimum CPU
instruction capability, downloads an explicitly requested Hugging Face
snapshot, verifies its deterministic content digest, and starts one selected
model behind the stable OpenAI proxy. It remains intentionally CPU-only.

`acc-llm-arm64` packages a pinned arm64 vLLM image and the same non-root
manager. Its package is selected explicitly; it is not included by `all` while
we maintain the standard AMD64 delivery baseline. CUDA is still contingent on
Kubernetes GPU-resource exposure and the manager's in-container confirmation.
Each release must validate OpenAI streaming, model switching, persistence,
rollback, backup/restore, and interrupted-download cleanup for the exact image
digest it publishes.
