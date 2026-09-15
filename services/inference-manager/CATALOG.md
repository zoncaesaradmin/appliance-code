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
hardcoded. Runtime mode, host available memory, container cgroup memory limits,
GPU free memory for CUDA, and PVC free space determine current eligibility.
GPU memory is not summed across devices; automatic tensor parallelism is not
configured. Conservatively reserve 25% of CPU memory (20% of GPU memory), require
twice the weights plus 2 GiB (Ollama) or 4 GiB (vLLM), and twice download storage.
vLLM catalog launches use a 2048-token context. Estimates are recomputed locally
on every catalog read and selection. Cached Hugging Face revisions can become
unavailable upstream; report a download error without changing installed models.

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

The vLLM serving child uses local model paths with `HF_HUB_OFFLINE=1` and
`TRANSFORMERS_OFFLINE=1`; usage reporting is disabled with
`HF_HUB_DISABLE_TELEMETRY=1`, `VLLM_NO_USAGE_STATS=1`, and `DO_NOT_TRACK=1`.
These overrides apply only to serving, preserving upstream catalog refreshes
and administrator-triggered model downloads in the manager. An offline refresh
failure retains the previous catalog and never removes installed models.

`GET /internal/v1/models/catalog` returns cached candidates and eligibility.
`GET /v1/models` independently lists downloaded models. The control plane exposes
the catalog as `GET /api/v1/inference/models/catalog`. Catalog imports include
`catalogId` alongside matching `modelId` and `source`; the manager revalidates
capacity and takes launch arguments from the cache. Explicit imports retain
the existing administrator API for advanced uses.

Unit tests exercise the real `discoverVLLM` / `discoverOllama` path against a
local HTTPS fixture (no public network required) and prove architecture probing
parses the installed registry without importing the vLLM runtime.

UI download state is derived from the runtime inventory, never inferred from
catalog membership. Removing or losing a catalog entry does not remove its
download or prevent loading/deleting it. Offline catalog status includes the
last successful refresh and failure so cached metadata is not presented as fresh.
