#!/usr/bin/env bash
# Product-level TARGET_ARCH / HOST_ARCH for package export scripts.
# Source from scripts/package/*.sh — not a standalone CLI.
#
# Env:
#   TARGET_ARCH   required: amd64|arm64 (no default — fail closed if unset)
#   HOST_ARCH     optional: amd64|arm64; when unset, detect via go/uname
#
# After target_arch_resolve / host_arch_resolve: values are normalized and exported.
#
# Multi-arch packaging invariant:
#   HOST_ARCH  = BUILDPLATFORM (native compile / Node frontend tooling)
#   TARGET_ARCH = product OCI platform (runtime image / skopeo --override-arch)
# Never treat TARGET_ARCH as a silent fallback for HOST_ARCH.

_normalize_arch() {
  local label="$1"
  local raw="$2"
  raw="$(printf '%s' "${raw}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  case "${raw}" in
    amd64|x86_64|x86-64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *)
      echo "target-arch: unsupported ${label}=${raw:-(empty)} (want amd64|arm64)" >&2
      return 2
      ;;
  esac
}

target_arch_resolve() {
  local raw="${TARGET_ARCH-}"
  if [[ -z "$(printf '%s' "${raw}" | tr -d '[:space:]')" ]]; then
    echo "target-arch: TARGET_ARCH is required (amd64|arm64); no default" >&2
    return 2
  fi
  TARGET_ARCH="$(_normalize_arch TARGET_ARCH "${raw}")" || return 2
  export TARGET_ARCH
}

host_arch_resolve() {
  local raw="${HOST_ARCH-}"
  if [[ -z "$(printf '%s' "${raw}" | tr -d '[:space:]')" ]]; then
    if command -v go >/dev/null 2>&1; then
      raw="$(go env GOARCH 2>/dev/null || true)"
    fi
  fi
  if [[ -z "$(printf '%s' "${raw}" | tr -d '[:space:]')" ]]; then
    raw="$(uname -m 2>/dev/null || true)"
  fi
  if [[ -z "$(printf '%s' "${raw}" | tr -d '[:space:]')" ]]; then
    echo "target-arch: HOST_ARCH is required (amd64|arm64); set HOST_ARCH or ensure go/uname is available" >&2
    return 2
  fi
  HOST_ARCH="$(_normalize_arch HOST_ARCH "${raw}")" || return 2
  export HOST_ARCH
}
