#!/usr/bin/env bash
set -euo pipefail

usage() { echo "usage: export-postgresql-image-archive.sh --out-file PATH [--reference-out-file PATH] [--source-image REF]"; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE="${POSTGRES_SOURCE_IMAGE:-docker.io/library/postgres:16.15-alpine3.24}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-postgresql-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "${OUT_FILE}" ]] || { echo "export-postgresql-image-archive: --out-file is required" >&2; exit 2; }
args=(--repository postgres --source-image "${SOURCE_IMAGE}" --out-file "${OUT_FILE}")
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then args+=(--reference-out-file "${REFERENCE_OUT_FILE}"); fi
exec "${SCRIPT_DIR}/export-oci-image-archive.sh" "${args[@]}"
