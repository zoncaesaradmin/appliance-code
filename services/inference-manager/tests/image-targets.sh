#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Exercise the real recursive targets without pulling, building, or pushing.
unset SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG BASE_IMAGE GOARCH
unset SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO
plan="$(make --no-print-directory -n image \
  DEV_REGISTRY=registry.example DEV_REGISTRY_USER=test DEV_REGISTRY_TOKEN=test \
  VERSION=contract-test)"
[[ "$plan" == *"GOARCH=amd64 go build"* ]]
[[ "$plan" == *"--arch amd64 --build-arg BASE_IMAGE="* ]]
[[ "$plan" == *"registry.example/appliance-images/appliance-inference-manager:contract-test"* ]]
[[ "$plan" == *"BASE_IMAGE=docker.io/library/alpine:3.24.1"* ]] || [[ "$plan" == *"BASE_IMAGE="*"alpine"* ]]

# Bundle packaging overrides names and bases; retain its canonical local tag.
plan="$(make --no-print-directory -n image-local \
  SERVICE_IMAGE_NAME=localhost/inference-manager SERVICE_IMAGE_TAG=bundled \
  BASE_IMAGE=localhost/preloaded:bundled BUILD_ENGINE='buildah bud --pull-never')"
[[ "$plan" == *"buildah bud --pull-never"* ]]
[[ "$plan" == *"-t localhost/inference-manager:bundled"* ]]
echo 'inference-manager image target contracts passed'
