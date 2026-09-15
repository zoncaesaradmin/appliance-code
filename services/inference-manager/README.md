# Inference image development

Use the same registry environment as the control-plane image workflow:

```sh
make dev-shell
make -C services/inference-manager image
```

The default builds the pinned Ollama amd64 image with the appliance manager,
pushes it using the shared service-image rules, and prints the image reference.
Use the printed versioned reference for the `inference-runtime` container in
the `inference/inference-gateway` Deployment. Each invocation has a new dev tag;
`SERVICE_IMAGE_TAG` can override it. Direct Deployment edits are development
overrides and can be replaced by the next appliance/Helm upgrade.

Explicit targets cover each supported package:

```sh
make -C services/inference-manager image-ollama-amd64
make -C services/inference-manager image-vllm-amd64
make -C services/inference-manager image-vllm-arm64
```

The default image names end in `-ollama-amd64`, `-vllm-amd64`, or `-vllm-arm64`,
so both versioned tags and `latest` remain separate for each runtime.
The Go binary and container image use the selected architecture. These builds
only copy files into the pinned engine base, so an amd64 build host can produce
the arm64 image without executing arm64 programs. It must run on an arm64 target.
vLLM amd64 uses the CPU base; vLLM arm64 uses the CUDA base.

`BASE_IMAGE` overrides the pinned engine base. Offline builds must specify a
seeded LAN or preloaded local base; public base references fail closed.
`image-local` builds without pushing. Signed bundle packaging continues to use
`image-local` with its preloaded base and explicit version.
