#!/usr/bin/env bash
set -euo pipefail

usage() { echo "usage: export-message-broker-image-archive.sh --out-file PATH [--reference-out-file PATH] [--source-image REF]"; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/oci-pull.sh"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE="${MESSAGE_BROKER_SOURCE_IMAGE:-nats:2.10.26-alpine}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-message-broker-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "${OUT_FILE}" ]] || { echo "export-message-broker-image-archive: --out-file is required" >&2; exit 2; }
for tool in skopeo python3 tar; do command -v "${tool}" >/dev/null || { echo "export-message-broker-image-archive: ${tool} is required" >&2; exit 1; }; done
mkdir -p "$(dirname "${OUT_FILE}")"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "localhost/appliance-message-broker:2.10.26"
skopeo copy --override-os linux --override-arch amd64 containers-storage:localhost/appliance-message-broker:2.10.26 "oci:${tmp}/oci:registry.local/nats:bundled"
digest="$(python3 - "${tmp}/oci/index.json" <<'PY'
import json, sys
index=json.load(open(sys.argv[1], encoding="utf-8"))
manifests=index.get("manifests", [])
if len(manifests) != 1: raise SystemExit("message-broker image must contain one linux/amd64 manifest")
d=manifests[0]
if (d.get("annotations") or {}).get("org.opencontainers.image.ref.name") != "registry.local/nats:bundled": raise SystemExit("message-broker archive annotation mismatch")
digest=d.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71: raise SystemExit("invalid message-broker image digest")
print(digest)
PY
)"
tar -C "${tmp}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"; printf 'registry.local/nats@%s\n' "${digest}" >"${REFERENCE_OUT_FILE}"; fi
echo "created message-broker image archive: ${OUT_FILE}"
