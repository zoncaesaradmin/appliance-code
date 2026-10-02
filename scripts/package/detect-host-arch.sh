#!/usr/bin/env bash
# Print the host machine arch as amd64|arm64 for Makefile HOST_ARCH.
# Prefer go when present; bare build hosts often have no host Go and fall
# back to uname. Prints nothing (and exits 1) when detection fails.
set -euo pipefail

arch=""
if command -v go >/dev/null 2>&1; then
  arch="$(go env GOARCH 2>/dev/null || true)"
fi
if [[ -z "${arch}" ]]; then
  arch="$(uname -m 2>/dev/null || true)"
fi
case "${arch}" in
  x86_64|amd64)
    printf 'amd64\n'
    ;;
  aarch64|arm64)
    printf 'arm64\n'
    ;;
  *)
    exit 1
    ;;
esac
