#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Exercise the real recursive targets without pulling, building, or pushing.
unset SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG BASE_IMAGE GOARCH TARGET_ARCH
unset SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO

# Empty GOARCH/TARGET_ARCH must fail closed (no amd64 default).
# Use require-goarch (not make -n) so the shell check actually runs.
# Clear any GOARCH inherited via MAKEFLAGS from a parent `make test GOARCH=...`.
if make --no-print-directory require-goarch GOARCH= TARGET_ARCH= \
  >/tmp/inference-manager-arch.out 2>/tmp/inference-manager-arch.err; then
  echo 'inference-manager: expected require-goarch without GOARCH/TARGET_ARCH to fail' >&2
  exit 1
fi
grep -q 'GOARCH or TARGET_ARCH is required' /tmp/inference-manager-arch.err

plan="$(make --no-print-directory -n image \
  DEV_REGISTRY=registry.example DEV_REGISTRY_USER=test DEV_REGISTRY_TOKEN=test \
  VERSION=contract-test GOARCH=amd64)"
[[ "$plan" == *"GOARCH=amd64 go build"* ]]
[[ "$plan" == *"--arch amd64 --build-arg BASE_IMAGE="* ]]
[[ "$plan" == *"registry.example/appliance-images/appliance-inference-manager:contract-test"* ]]
[[ "$plan" == *"BASE_IMAGE=docker.io/library/alpine:3.24.1"* ]] || [[ "$plan" == *"BASE_IMAGE="*"alpine"* ]]

plan="$(make --no-print-directory -n image \
  DEV_REGISTRY=registry.example DEV_REGISTRY_USER=test DEV_REGISTRY_TOKEN=test \
  VERSION=contract-test TARGET_ARCH=arm64)"
[[ "$plan" == *"GOARCH=arm64 go build"* ]]
[[ "$plan" == *"--arch arm64 --build-arg BASE_IMAGE="* ]]

# Bundle packaging overrides names and bases; retain its canonical local tag.
plan="$(make --no-print-directory -n image-local \
  SERVICE_IMAGE_NAME=localhost/inference-manager SERVICE_IMAGE_TAG=bundled \
  BASE_IMAGE=localhost/preloaded:bundled BUILD_ENGINE='buildah bud --pull-never' \
  GOARCH=amd64)"
[[ "$plan" == *"buildah bud --pull-never"* ]]
[[ "$plan" == *"-t localhost/inference-manager:bundled"* ]]
echo 'inference-manager image target contracts passed'
