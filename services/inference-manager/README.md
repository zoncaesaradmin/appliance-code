# Inference manager

Thin lifecycle/API manager for appliance inference. Upstream engine images
(Ollama / vLLM) are packaged separately as `inference-runtime` and run as a
sidecar in the same pod; this image does not wrap those bases.

```sh
make -C services/inference-manager build
make -C services/inference-manager image-local
```

Packaging uses `make package-inference-manager-image-archive`, which annotates
`registry.local/inference-manager:bundled` and emits a digest-pinned reference.
The Helm chart mounts both `managerImage` and `image` (engine) digests from
zonctl install values.

Legacy Make aliases `image-ollama-amd64`, `image-vllm-amd64`, and
`image-vllm-arm64` all build the same thin manager; engine selection is pack
metadata plus the separate runtime archive.
