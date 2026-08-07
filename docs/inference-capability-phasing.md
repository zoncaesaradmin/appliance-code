# Inference Capability Phasing

This note captures the rollout split for the optional local LLM inference
capability. Implementation naming uses `inference` for capability, module,
API paths, and permissions. Product-facing profiles use `lanllm` (parallel to
`landns` for DNS).

## Decisions (Phase 1)

- **Surface:** inference only — OpenAI-compatible chat/completions and models
  list, gated by capability
- **Models:** runtime (+ gateway Service) ship in the main air-gap bundle;
  **model weights** ship as a separate signed **model pack**
- **Not in this phase:** coding-agent tools, RAG/vector DB, Open WebUI, cloud
  LLM fallback
- **Default backend:** Ollama-compatible runtime; product names stay
  runtime-agnostic behind `inference-gateway`

## Naming

| Layer | Name | Meaning |
|---|---|---|
| Capability | `inference` | Local LLM inference APIs exist on this appliance |
| Module | `inference-runtime` | Cluster service: gateway + inference runtime |
| Profiles | `lanllm`, `builder-lanllm`, `builder-lanllm-storage-landns` | Inference-only; builder ∪ inference; full union |
| Stable in-cluster URL | `http://inference-gateway.inference.svc.cluster.local:8080` | Swap Ollama/vLLM/LiteLLM without changing the control plane |
| Public API | `/inference/v1/*` (OpenAI-compatible) | External clients; appliance Bearer / `apt_` token |
| Permissions | `inference.use`, `inference.models.read`, `inference.admin` | Completions; list models; manage packs/runtime |

`lanllm` is the product face for capability `inference` (like `landns` for
`dns`). `builder-lanllm` = builder ∪ lanllm.
`builder-lanllm-storage-landns` = builder ∪ storage/registry ∪ landns ∪ lanllm
(full capability set).

## Slice A — Capability / profile wiring (no workload yet)

Mirror LAN DNS Phase 1 wiring:

- Add `CapabilityInference = "inference"` and profiles `lanllm` /
  `builder-lanllm` / `builder-lanllm-storage-landns` in control-plane and
  `zonctl` productconfig catalogs
- Capability deps: `inference` → `base` (and `host` in metadata YAML for
  consistency with dns)
- Module `inference-runtime`: `ExecutionModeClusterService`, stable `BaseURL`,
  proxy routes for `/inference/v1/models` and `/inference/v1/chat/completions`
- Metadata catalogs under `metadata-bundle/base/` + sync/embed
- Docs and schema enums so the new profiles are accepted and reported
- Tests: resolve profile/modules; non-inference profiles do not enable the
  module

Slice A does **not** require a running inference pod.

## Slice B — Runtime chart + install gates + OpenAI proxy

Follow the artifact/DNS vertical:

1. **Chart** `deploy/charts/appliance-inference` (namespace `inference`):
   - ClusterIP Service `inference-gateway` (port 8080 → runtime 11434)
   - Deployment (single replica, Recreate), no `hostNetwork`
   - Models PVC (empty at install; filled by model-pack import)
   - Non-root UID/GID `10005` / shared `fsGroup` `20000`, RO rootfs,
     Restricted-PSA friendly
2. **Bundle contract** (first-class):
   - `inferenceRuntimeImage` / `inferenceChart` /
     `compatibility.inferenceVersion`
   - Annotation `registry.local/inference-runtime:bundled` + digest pin
     `registry.local/inference-runtime@sha256:…`
3. **zonctl install:** capability-gated preload/Helm before control plane;
   inject `config.inferenceGatewayBaseURL`; refuse in-place
   inference → non-inference upgrades
4. **Control plane:** fail-closed when capability is on and base URL is
   empty; authz + reverse-proxy to the gateway; Traefik path through CP;
   permissions in roles catalog
5. **Verify:** profile-matrix evidence for inference vs non-inference
   profiles

## Slice C — Signed model pack (separate from main bundle)

See [inference-model-packs.md](inference-model-packs.md) for the pack
format, `zonctl models-import`, release publish path, and the ~30 GB Qwen
reference guidance.

Summary:

- **Model pack format** (`appliance.modelpack/v1`): signed `manifest.json` + blobs
- **CLI:** `zonctl models-import --bundle-dir <extracted-pack>`
- **Host path:** `/data/zon/inference/models` (UID `10006`, shared GID `20000`)
- Main `build-full-bundle` does **not** embed weights

## Intent

Stabilize names, profiles, and installer contracts first (Slice A), then land
the runtime chart and OpenAI proxy (Slice B) with an empty models PVC, then
deliver signed model packs outside the main air-gap bundle (Slice C).

## Explicit non-goals (Phase 1)

- Coding-agent module, workspace tool loop, MCP inference tools beyond
  existing `/mcp`
- RAG / embeddings / vector DB
- Open WebUI
- Cloud model providers / LiteLLM multi-backend (Stable Service URL leaves
  room for later)
- GPU scheduling policy beyond documenting host requirements

## Follow-on (out of Phase 1)

- Module `coding-agent` for workspace coding-agent behavior
- LiteLLM or vLLM swap behind `inference-gateway`
- Minimal appliance UI chat page
- RAG service + vector store
