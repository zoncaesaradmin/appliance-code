#!/usr/bin/env bash
# Product-level TARGET_ARCH for package export scripts.
# Source from scripts/package/*.sh — not a standalone CLI.
#
# Env:
#   TARGET_ARCH   required: amd64|arm64 (no default — fail closed if unset)
#
# After target_arch_resolve: TARGET_ARCH is normalized and exported.

target_arch_resolve() {
  local raw="${TARGET_ARCH-}"
  raw="$(printf '%s' "${raw}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  if [[ -z "${raw}" ]]; then
    echo "target-arch: TARGET_ARCH is required (amd64|arm64); no default" >&2
    return 2
  fi
  case "${raw}" in
    amd64|x86_64|x86-64)
      TARGET_ARCH="amd64"
      ;;
    arm64|aarch64)
      TARGET_ARCH="arm64"
      ;;
    *)
      echo "target-arch: unsupported TARGET_ARCH=${raw} (want amd64|arm64)" >&2
      return 2
      ;;
  esac
  export TARGET_ARCH
}
