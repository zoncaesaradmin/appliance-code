#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Exercise the real recursive targets without pulling, building, or pushing.
# Clear inherited service settings so this checks the defaults deterministically.
unset SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG BASE_IMAGE INFERENCE_ENGINE GOARCH
unset SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO
for variant in ollama-amd64 vllm-amd64 vllm-arm64; do
  architecture="${variant##*-}"
  plan="$(make --no-print-directory -n "image-${variant}" \
    DEV_REGISTRY=registry.example DEV_REGISTRY_USER=test DEV_REGISTRY_TOKEN=test \
    VERSION=contract-test)"
  [[ "$plan" == *"GOARCH=${architecture} go build"* ]]
  [[ "$plan" == *"--arch ${architecture} --build-arg BASE_IMAGE="* ]]
  [[ "$plan" == *"registry.example/appliance-images/appliance-inference-runtime-${variant}:contract-test"* ]]
  case "$variant" in
    ollama-amd64) [[ "$plan" == *"BASE_IMAGE=docker.io/ollama/ollama:"* ]] ;;
    vllm-amd64) [[ "$plan" == *"BASE_IMAGE=docker.io/vllm/vllm-openai-cpu:"* ]] ;;
    vllm-arm64) [[ "$plan" == *"BASE_IMAGE=docker.io/vllm/vllm-openai:"* ]] ;;
  esac
done

# Bundle packaging overrides names and bases; retain its canonical local tag.
plan="$(make --no-print-directory -n image-local \
  SERVICE_IMAGE_NAME=localhost/appliance-inference-runtime SERVICE_IMAGE_TAG=bundled \
  BASE_IMAGE=localhost/preloaded:bundled BUILD_ENGINE='buildah bud --pull-never')"
[[ "$plan" == *"buildah bud --pull-never"* ]]
[[ "$plan" == *"-t localhost/appliance-inference-runtime:bundled"* ]]
echo 'inference image target contracts passed'
