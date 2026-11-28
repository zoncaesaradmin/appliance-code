#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE="${POSTGIS_SOURCE_IMAGE:-docker.io/postgis/postgis:16-3.7@sha256:c493152a135059753aef4d8db20c9c55a11aaeae0768e8d7710c23df47be89f2}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --help|-h) echo "usage: export-postgis-image-archive.sh --out-file PATH [--reference-out-file PATH] [--source-image REF]"; exit 0 ;;
    *) echo "export-postgis-image-archive.sh: unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -n "${OUT_FILE}" ]] || { echo "export-postgis-image-archive.sh: --out-file is required" >&2; exit 2; }
args=(--repository postgis --source-image "${SOURCE_IMAGE}" --out-file "${OUT_FILE}")
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then args+=(--reference-out-file "${REFERENCE_OUT_FILE}"); fi
exec "${SCRIPT_DIR}/export-oci-image-archive.sh" "${args[@]}"
