#!/usr/bin/env bash
# Dry-run regression check for make DEV_RUN nested containers-storage mounts.
# Does not start containers; only asserts Makefile flag expansion.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

fail() {
  echo "test-dev-run-storage-mounts: $*" >&2
  exit 1
}

dry_run_dev_run() {
  local driver="$1"
  # Suppress auth/sudo bootstrap noise; we only need the printed podman/docker line.
  make -n \
    SUDO= \
    CONTAINER_ENGINE=podman \
    DEV_STORAGE_DRIVER="${driver}" \
    DEV_CACHE_DIR=/tmp/appliance-code-dev-cache-test \
    DEV_VOLUME_OPTS= \
    SCRIPT=scripts/package/oci-pull.sh \
    dev-run 2>&1
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  [[ "${haystack}" == *"${needle}"* ]] || fail "expected dry-run output to contain: ${needle}"
}

out="$(dry_run_dev_run overlay)"
assert_contains "${out}" 'STORAGE_DRIVER="overlay"'
assert_contains "${out}" '--privileged'
assert_contains "${out}" '--device /dev/fuse'
assert_contains "${out}" '/tmp/appliance-code-dev-cache-test/containers/overlay/user:/home/devcontainer/.local/share/containers'
assert_contains "${out}" '/tmp/appliance-code-dev-cache-test/containers/overlay/system:/var/lib/containers'
assert_contains "${out}" '/tmp/appliance-code-dev-cache-test/go-build:/home/devcontainer/.cache/go-build'
assert_contains "${out}" 'mkdir -p "/tmp/appliance-code-dev-cache-test/go-build"'

out="$(dry_run_dev_run vfs)"
assert_contains "${out}" 'STORAGE_DRIVER="vfs"'
assert_contains "${out}" '/tmp/appliance-code-dev-cache-test/containers/vfs/user:/home/devcontainer/.local/share/containers'
assert_contains "${out}" '/tmp/appliance-code-dev-cache-test/containers/vfs/system:/var/lib/containers'

# Cross-arch packaging: outer tooling stays host-native; product TARGET_ARCH is
# forwarded for GOARCH / buildah --arch (nested buildah under qemu fails).
host_arch="$(go env GOARCH)"
case "${host_arch}" in
  amd64) foreign_arch=arm64 ;;
  arm64) foreign_arch=amd64 ;;
  *) fail "unsupported host Go arch ${host_arch}" ;;
esac
cross_out="$(make -n \
  SUDO= \
  CONTAINER_ENGINE=podman \
  DEV_STORAGE_DRIVER=overlay \
  DEV_CACHE_DIR=/tmp/appliance-code-dev-cache-test \
  DEV_VOLUME_OPTS= \
  TARGET_ARCH="${foreign_arch}" \
  DEV_IMAGE_TAG="latest-${foreign_arch}" \
  SCRIPT=scripts/package/oci-pull.sh \
  dev-run 2>&1)"
assert_contains "${cross_out}" "--arch ${host_arch}"
assert_contains "${cross_out}" "TARGET_ARCH=\"${foreign_arch}\""
assert_contains "${cross_out}" "dev-build:latest-${host_arch}"

# Platform build-args for native cross-compile (BUILDPLATFORM + TARGETARCH).
platform_out="$(make -n -C services/controlplane \
  SUDO= \
  TARGET_ARCH="${foreign_arch}" \
  HOST_ARCH="${host_arch}" \
  SERVICE_IMAGE_NAME=localhost/appliance-control-plane \
  SERVICE_IMAGE_TAG=test \
  VERSION=test \
  image-local 2>&1)"
assert_contains "${platform_out}" "--build-arg BUILDPLATFORM=linux/${host_arch}"
assert_contains "${platform_out}" "--build-arg TARGETARCH=${foreign_arch}"
assert_contains "${platform_out}" "--build-arg TARGETPLATFORM=linux/${foreign_arch}"

if make -n DEV_STORAGE_DRIVER=btrfs SCRIPT=scripts/package/oci-pull.sh dev-run >/dev/null 2>&1; then
  fail "expected DEV_STORAGE_DRIVER=btrfs to be rejected"
fi

echo "test-dev-run-storage-mounts: ok"
