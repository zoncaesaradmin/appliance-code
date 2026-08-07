# Inference Model Packs

Model weights are **not** part of the main signed air-gap appliance bundle.
They ship as a separate signed **model pack** that operators import after
install (or as a day-2 step) with `zonctl models-import`.

## Why separate packs

- Platform releases stay small and update independently of multi‑GB weights.
- Operators can choose which models to place on a given host (for example a
  ~30 GB RAM class host running a Qwen coding model).
- The inference runtime image/chart remain swappable behind
  `inference-gateway` without rebaking weights into the platform bundle.

## Pack layout (appliance.modelpack/v1)

Extracted directory:

```text
manifest.json
manifest.json.sig          # ed25519 detached signature of manifest.json
blobs/
  <weight-file>            # content-addressed blob(s)
```

`manifest.json` fields:

| Field | Meaning |
|---|---|
| `schemaVersion` | `1` |
| `kind` | `appliance.modelpack/v1` |
| `modelId` | OpenAI-compatible model name clients pass (for example `qwen2.5-coder:14b`) |
| `runtime` | Initial value `ollama` (product stays runtime-agnostic at the API layer) |
| `digest` | Aggregate/content digest for the pack |
| `sizeBytes` | Sum of blob sizes |
| `minRAMGB` | Operator guidance (for example `30` for a 14B–30B class coding model) |
| `compatibility.inferenceVersion` | Must match the appliance `compatibility.inferenceVersion` |
| `blobs[]` | `{path, digest, sizeBytes}` entries verified on import |

## Import on the appliance

```bash
# Pack directory already extracted on the target host.
sudo zonctl models-import \
  --bundle-dir /path/to/extracted-modelpack \
  --public-key /etc/zon/keys/release-signing.pub
```

Behavior:

1. Verify `manifest.json` (+ signature when a public key is supplied).
2. Verify every blob digest/size.
3. Ensure `/data/zon/inference/models` exists with UID/GID `10006:20000` mode `2770`.
4. Copy blobs into `/data/zon/inference/models/<sanitized-modelId>/`.

The inference Deployment mounts that host path at `/models` (`OLLAMA_MODELS`).
Install without a pack still succeeds: the gateway comes up and
`GET /inference/v1/models` may be empty until a pack is imported.

## Publishing packs (release skill)

Model packs are published **alongside** platform bundles, not inside
`build-full-bundle.sh`.

Suggested release-host recipe:

1. Pull/export the chosen Ollama model blob set offline (or from an
   approved internal cache).
2. Write `manifest.json` with `compatibility.inferenceVersion` equal to the
   platform release’s inference version.
3. Sign `manifest.json` with the same release-signing key used for bundles.
4. Upload the pack archive to the DEV_REGISTRY files API (or equivalent)
   under a versioned path such as
   `model-packs/<inferenceVersion>/<modelId>.tar.zst`.
5. Record the pack digest and `minRAMGB` in release notes; do **not** add
   the weights to `release-input.json` platform artifacts.

## Reference: Qwen pack for ~30 GB hosts

| Field | Suggested value |
|---|---|
| `modelId` | `qwen2.5-coder:14b` (or a 30B-class MoE variant when the host allows) |
| `runtime` | `ollama` |
| `minRAMGB` | `30` |
| Host class | ~30 GB RAM appliance (CPU or modest GPU) |
| Notes | Prefer a coding-tuned Qwen build with tool-calling support if later enabling the coding-agent module |

Exact quantized blob names and digests are produced when the pack is built;
this document only fixes the operator contract and sizing guidance.

## Non-goals

- Embedding weights in the platform air-gap bundle
- Cloud model providers / LiteLLM multi-backend in this pack format
- Automatic public download of models during install
