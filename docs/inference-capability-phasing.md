# Standard and Accelerated Inference

The current implementation is standard CPU inference: capability `inference`
is delivered by the signed `std-llm-amd64` package and runs the pinned Ollama engine.
The next implementation will use the same `inference` capability and package
`acc-llm-arm64`. That package is reserved for planning; it is not yet published or
selectable.

## Current contract

| Layer | Current name |
| --- | --- |
| Capability | `inference` (Standard Inference) |
| Capability entitlement | `zon.capabilities.inference` |
| Delivery package | `std-llm-amd64` |
| Archive | `appliance-<version>-std-llm-amd64.tar.gz` |
| Canonical profile enabling it | `builder-lanllm-storage-landns` |
| Module / container | `inference-runtime` |
| Chart / Helm release | `appliance-inference` |
| Deployment / Service | `inference-gateway` in namespace `inference` |
| In-cluster URL | `http://inference-gateway.inference.svc.cluster.local:8080` |
| Public API | `/inference/v1/models`, `/inference/v1/chat/completions` |
| Permissions | `inference.use`, `inference.models.read`, `inference.admin` |

The profile ID remains unchanged; its metadata capability set now contains
`inference`. Other profiles stay unchanged. Profiles are selected from the
signed metadata catalog, and the release index projects their capabilities onto
delivery packages. Having a package available never enables its capability.

`inference` requires `base`. It gates the existing module, image preload,
Helm install, gateway configuration, authenticated proxy routes, and readiness
configuration. The current chart explicitly disables NVIDIA and AMD GPU
visibility using `CUDA_VISIBLE_DEVICES=-1` and `ROCR_VISIBLE_DEVICES=-1`, following
the [pinned Ollama GPU selection documentation](https://github.com/ollama/ollama/blob/v0.6.5/docs/gpu.md).
It requests CPU and memory only. The upstream image is unchanged and may contain
GPU libraries; this phase establishes CPU execution, not a stripped image build.

Release inputs retain `inferenceRuntimeImage`, `inferenceChart`, and
`compatibility.inferenceVersion`. OCI archives retain
`registry.local/inference-runtime:bundled` and the verified platform-manifest
digest reference `registry.local/inference-runtime@sha256:...`. Assembly places
the runtime and chart in `std-llm-amd64`; zonctl verifies, imports, and tags that image
before installing the shared chart. Online packaging pulls the pinned upstream;
offline packaging consumes the existing `deps/inference` LAN seed without an
upstream fallback.

## Upgrade and operator changes

Change explicit `APPLIANCE_PACKS` / `build_flow.appliance_packs` selections from
`inference` to `std-llm-amd64`. For example, use `foundation,std-llm-amd64` for a metadata
profile requiring only base and standard inference; the canonical full builder
profile also needs `dev-platform` and `deviceuser`. Regenerate assembly configs
with `bundle-assembly.std-llm-amd64.json`, package the updated signed metadata, and
publish the corresponding release index and packs together. `all` includes
`std-llm-amd64`. The old package ID is rejected with migration guidance.

The capability ID and entitlement key are a coordinated metadata/software
change. Metadata and offline licenses using the old capability name must be
reissued for the new contract; no implicit alias grants standard or accelerated
inference. Existing profile IDs, API paths, permissions, Helm identity, model
storage, and UID/GID remain stable. Existing installations need the new signed
release and matching metadata to see the rename.

Model weights remain separately signed model packs. The CPU backend keeps
`/data/zon/inference/models`, UID `10006`, shared GID `20000`, and the current
Ollama import contract. See [inference-model-packs.md](inference-model-packs.md).

## Accelerated follow-up

The proposed GPU engine is vLLM, subject to selecting and pinning the supported
hardware/backend version. It provides an
[OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/),
which can preserve the existing client models/chat API. Compatibility tests must
cover streaming, errors, model IDs, and supported request options before release.

Keep the same Deployment, Service, module, and external API. Switching engines
replaces the pod with a different digest-pinned image; it cannot retain the exact
Kubernetes pod name or UID. The runtime chart needs explicit engine-specific
startup arguments, ports, probes, and GPU resource configuration. A Service name
alone does not make the implementations interchangeable.

Before enabling `acc-llm-arm64`:

- Publish a separate metadata bundle/package set in which `acc-llm-arm64` is the
  single package providing `inference`. Profiles continue to require only the
  shared capability.
- Define separate signed CPU/GPU artifact pins and unambiguous package ownership,
  updating producers, schemas, validators, image preload, Helm values, and status.
- Seed every new GPU image, driver/toolkit, and device-plugin dependency for
  offline use, with the same pinned upstream inputs in online builds.
- Validate supported GPU hardware, offline driver/runtime provisioning, and
  device allocation before deploying; retain the non-root/storage boundaries.
- Define backend-specific signed model-pack compatibility and migration. Do not
  assume Ollama's model files can be consumed unchanged by vLLM.
- Exercise install, CPU/GPU transition, rollback, backup/restore, and machine
  migration, including model storage preservation and observable inference.

No GPU package, GPU profile, driver provisioning, or engine switch is implemented
in this rename phase.
