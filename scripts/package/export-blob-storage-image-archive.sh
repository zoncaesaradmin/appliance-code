#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-blob-storage-image-archive.sh --out-file PATH [options]

Re-exports the pinned S3-compatible blob-storage image as a Linux OCI
archive for TARGET_ARCH (amd64|arm64, required), annotated for the
appliance's offline registry.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write registry.local/blob-storage@sha256:... here.
  --source-image REF        Upstream image. Defaults to minio/minio:<version>.
  --blob-storage-version V  Defaults to the control-plane chart image tag.

Environment:
  TARGET_ARCH               amd64|arm64 (required; no default).
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/oci-pull.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
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
SOURCE_ID_FILE="${OUT_FILE}.source-id"
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  REFERENCE_FILE="$(cd "$(dirname "${REFERENCE_OUT_FILE}")" && pwd)/$(basename "${REFERENCE_OUT_FILE}")"
else
  REFERENCE_FILE="${OUT_FILE}.reference"
fi

resolve_source_digest() {
  local digest=""
  digest="$(skopeo inspect --override-os linux --override-arch "${TARGET_ARCH}" \
    --format '{{.Digest}}' "containers-storage:${LOCAL_REF}" 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}"
    return 0
  fi
  digest="$(skopeo inspect --override-os linux --override-arch "${TARGET_ARCH}" \
    --format '{{.Digest}}' "docker://${SOURCE_IMAGE}" 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}"
    return 0
  fi
  return 1
}

if [[ -f "${OUT_FILE}" && -f "${SOURCE_ID_FILE}" && -f "${REFERENCE_FILE}" ]]; then
  previous_id="$(tr -d '\r\n' <"${SOURCE_ID_FILE}" 2>/dev/null || true)"
  previous_ref="$(tr -d '\r\n' <"${REFERENCE_FILE}" 2>/dev/null || true)"
  if [[ -n "${previous_id}" && -n "${previous_ref}" ]]; then
    SOURCE_DIGEST="$(resolve_source_digest || true)"
    SOURCE_ID="${SOURCE_IMAGE}"
    if [[ -n "${SOURCE_DIGEST}" ]]; then
      SOURCE_ID="${SOURCE_IMAGE}@${SOURCE_DIGEST}"
    fi
    if [[ "${previous_id}" == "${SOURCE_ID}" ]]; then
      archive_digest="$(python3 - "${OUT_FILE}" <<'PY'
import json, sys, tarfile
archive = sys.argv[1]
with tarfile.open(archive) as tar:
    member = next(
        (e for e in tar.getmembers() if e.isfile() and e.name.lstrip("./") == "index.json"),
        None,
    )
    if member is None:
        raise SystemExit(f"oci archive {archive} is missing index.json")
    idx = json.load(tar.extractfile(member))
manifests = idx.get("manifests") or []
if not manifests:
    raise SystemExit(f"oci archive {archive} has no manifests")
digest = str(manifests[0].get("digest") or "").strip()
print(digest)
PY
)"
      expected_digest="${previous_ref##*@}"
      if [[ "${archive_digest}" != "${expected_digest}" ]]; then
        echo "blob-storage: stale reference sidecar ${previous_ref} (archive ${archive_digest}); rebuilding" >&2
      else
        echo "reusing blob-storage OCI archive: ${OUT_FILE}"
        echo "image reference: ${previous_ref}"
        exit 0
      fi
    fi
  fi
fi

oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "${LOCAL_REF}" "${TARGET_ARCH}"
SOURCE_DIGEST="$(skopeo inspect --override-os linux --override-arch "${TARGET_ARCH}" \
  --format '{{.Digest}}' "containers-storage:${LOCAL_REF}" 2>/dev/null || true)"
SOURCE_ID="${SOURCE_IMAGE}"
if [[ -n "${SOURCE_DIGEST}" ]]; then
  SOURCE_ID="${SOURCE_IMAGE}@${SOURCE_DIGEST}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" "containers-storage:${LOCAL_REF}" "oci:${TMP_DIR}/oci:registry.local/blob-storage:bundled"
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
printf '%s\n' "${SOURCE_ID}" >"${SOURCE_ID_FILE}"
printf '%s\n' "${REFERENCE}" >"${REFERENCE_FILE}"
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi
echo "created blob-storage OCI archive: ${OUT_FILE}"
echo "image reference: ${REFERENCE}"
