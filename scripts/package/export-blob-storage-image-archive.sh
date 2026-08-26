#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-blob-storage-image-archive.sh --out-file PATH [options]

Re-exports the pinned S3-compatible blob-storage image as a Linux/amd64 OCI
archive annotated for the appliance's offline registry.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write registry.local/blob-storage@sha256:... here.
  --source-image REF        Upstream image. Defaults to minio/minio:<version>.
  --blob-storage-version V  Defaults to the control-plane chart image tag.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/oci-pull.sh"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
BLOB_STORAGE_VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --blob-storage-version) BLOB_STORAGE_VERSION="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-blob-storage-image-archive: unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then echo "--out-file is required" >&2; exit 2; fi
for tool in skopeo python3 tar; do command -v "${tool}" >/dev/null || { echo "${tool} is required" >&2; exit 1; }; done
if [[ -z "${SOURCE_IMAGE}" || -z "${BLOB_STORAGE_VERSION}" ]]; then
  echo "--source-image and --blob-storage-version are required from signed release inputs" >&2
  exit 2
fi
mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
LOCAL_REF="localhost/appliance-blob-storage:${BLOB_STORAGE_VERSION}"
oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "${LOCAL_REF}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
skopeo copy --override-os linux --override-arch amd64 "containers-storage:${LOCAL_REF}" "oci:${TMP_DIR}/oci:registry.local/blob-storage:bundled"
DIGEST="$(python3 - "${TMP_DIR}/oci/index.json" <<'PY'
import json, sys
descriptor = json.load(open(sys.argv[1], encoding="utf-8"))["manifests"][0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/blob-storage:bundled":
    raise SystemExit("archive annotation is missing")
print(descriptor["digest"])
PY
)"
REFERENCE="registry.local/blob-storage@${DIGEST}"
rm -f "${OUT_FILE}"
tar -C "${TMP_DIR}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"; printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"; fi
echo "created blob-storage OCI archive: ${OUT_FILE}"
echo "image reference: ${REFERENCE}"
