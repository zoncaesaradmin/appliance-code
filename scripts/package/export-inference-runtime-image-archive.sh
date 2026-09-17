#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-inference-runtime-image-archive.sh --out-file PATH [options]

Re-exports the selected upstream inference engine image as an OCI archive
under the appliance install contract. The appliance manager is NOT layered
into this image; it ships separately as inference-manager.

The archive annotation is registry.local/inference-runtime:bundled and the
emitted workload reference is
registry.local/inference-runtime@sha256:<archive index digest>.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --source-image REF        Upstream image to re-export. Default:
                            engine-specific pinned image
  --inference-version VER   Compatibility version. Defaults to chart appVersion.
  --engine ENGINE           ollama (default) or vllm.
  --architecture ARCH       amd64|arm64. Required unless TARGET_ARCH is set.

Environment:
  TARGET_ARCH               Required when --architecture is omitted.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/oci-pull.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/target-arch.sh"
CHART_YAML="${REPO_ROOT}/deploy/charts/appliance-inference/Chart.yaml"
VLLM_VERSION_FILE="${REPO_ROOT}/services/inference-manager/version.env"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
INFERENCE_VERSION=""
ENGINE="ollama"
ARCHITECTURE=""
LOCAL_IMAGE_PREFIX="localhost"
IMAGE_NAME="inference-runtime"
PREFETCH_RETRIES=5

retry() {
  local attempt=1
  local max_attempts="$1"
  shift
  while true; do
    if "$@"; then
      return 0
    fi
    if (( attempt >= max_attempts )); then
      return 1
    fi
    sleep $((attempt * 2))
    attempt=$((attempt + 1))
  done
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --inference-version) INFERENCE_VERSION="${2:-}"; shift 2 ;;
    --engine) ENGINE="${2:-}"; shift 2 ;;
    --architecture) ARCHITECTURE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-inference-runtime-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${ARCHITECTURE}" ]]; then
  if [[ -z "${TARGET_ARCH-}" ]]; then
    echo "export-inference-runtime-image-archive: --architecture or TARGET_ARCH is required (amd64|arm64); no default" >&2
    exit 2
  fi
  target_arch_resolve
  ARCHITECTURE="${TARGET_ARCH}"
else
  TARGET_ARCH="${ARCHITECTURE}"
  target_arch_resolve
  ARCHITECTURE="${TARGET_ARCH}"
fi

case "${ENGINE}/${ARCHITECTURE}" in
  ollama/amd64|ollama/arm64|vllm/amd64|vllm/arm64) ;;
  *) echo "export-inference-runtime-image-archive: unsupported runtime ${ENGINE}/${ARCHITECTURE}" >&2; exit 2 ;;
esac

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-inference-runtime-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-inference-runtime-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${INFERENCE_VERSION}" && "${ENGINE}" == "ollama" ]]; then
  INFERENCE_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}")"
fi
if [[ -z "${INFERENCE_VERSION}" && "${ENGINE}" == "vllm" ]]; then
  # shellcheck disable=SC1090
  source "${VLLM_VERSION_FILE}"
  if [[ "${ARCHITECTURE}" == "arm64" ]]; then
    INFERENCE_VERSION="${VLLM_ARM64_VERSION}"
  else
    INFERENCE_VERSION="${VLLM_VERSION}"
  fi
fi
INFERENCE_VERSION="${INFERENCE_VERSION#v}"
if [[ -z "${INFERENCE_VERSION}" ]]; then
  echo "export-inference-runtime-image-archive: unable to derive inference version from ${CHART_YAML}" >&2
  exit 1
fi
IMAGE_TAG="${INFERENCE_VERSION}"
if [[ -z "${SOURCE_IMAGE}" ]]; then
  if [[ "${ENGINE}" == "vllm" ]]; then
    # shellcheck disable=SC1090
    source "${VLLM_VERSION_FILE}"
    if [[ "${ARCHITECTURE}" == "arm64" ]]; then
      SOURCE_IMAGE="${VLLM_ARM64_IMAGE}"
    else
      SOURCE_IMAGE="${VLLM_AMD64_IMAGE}"
    fi
  else
    SOURCE_IMAGE="docker.io/ollama/ollama:${INFERENCE_VERSION}"
  fi
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
LOCAL_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}-${ENGINE}:${IMAGE_TAG}"
SOURCE_ID_FILE="${OUT_FILE}.source-id"
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  REFERENCE_FILE="$(cd "$(dirname "${REFERENCE_OUT_FILE}")" && pwd)/$(basename "${REFERENCE_OUT_FILE}")"
else
  REFERENCE_FILE="${OUT_FILE}.reference"
fi

resolve_source_digest() {
  local digest=""
  digest="$(skopeo inspect --override-os linux --override-arch "${ARCHITECTURE}" \
    --format '{{.Digest}}' "containers-storage:${LOCAL_REF}" 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}"
    return 0
  fi
  digest="$(skopeo inspect --override-os linux --override-arch "${ARCHITECTURE}" \
    --format '{{.Digest}}' "docker://${SOURCE_IMAGE}" 2>/dev/null || true)"
  if [[ -n "${digest}" ]]; then
    printf '%s\n' "${digest}"
    return 0
  fi
  return 1
}

# Reuse an existing archive when the upstream pin is unchanged. Prefer a cheap
# digest probe (local storage, then registry inspect) so we can skip both the
# prefetch and the multi-gigabyte re-tar on rebuilds.
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
      # Fail closed if a freeze/copy left a stale .reference beside a newer tar.
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
        echo "inference-runtime: stale reference sidecar ${previous_ref} (archive ${archive_digest}); rebuilding" >&2
      else
        echo "reusing inference-runtime OCI archive: ${OUT_FILE}"
        echo "source image: ${SOURCE_IMAGE}"
        echo "runtime: ${ENGINE}/${ARCHITECTURE}"
        echo "archive annotation: registry.local/inference-runtime:bundled"
        echo "image reference: ${previous_ref}"
        exit 0
      fi
    fi
  fi
fi

# Prefetch the selected Linux architecture into local storage, then re-label under the
# canonical :bundled annotation. Do not layer the appliance manager into this image.
retry "${PREFETCH_RETRIES}" \
  oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "${LOCAL_REF}" "${ARCHITECTURE}"

SOURCE_DIGEST="$(skopeo inspect --override-os linux --override-arch "${ARCHITECTURE}" \
  --format '{{.Digest}}' "containers-storage:${LOCAL_REF}" 2>/dev/null || true)"
SOURCE_ID="${SOURCE_IMAGE}"
if [[ -n "${SOURCE_DIGEST}" ]]; then
  SOURCE_ID="${SOURCE_IMAGE}@${SOURCE_DIGEST}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

skopeo copy --override-os linux --override-arch "${ARCHITECTURE}" \
  "containers-storage:${LOCAL_REF}" "oci:${LAYOUT}:registry.local/inference-runtime:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/inference-runtime:bundled":
    raise SystemExit("OCI archive is missing registry.local/inference-runtime:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/inference-runtime@${DIGEST}"

rm -f "${OUT_FILE}"
tar -C "${LAYOUT}" -cf "${OUT_FILE}" oci-layout index.json blobs
printf '%s\n' "${SOURCE_ID}" >"${SOURCE_ID_FILE}"
printf '%s\n' "${REFERENCE}" >"${REFERENCE_FILE}"

echo "created inference-runtime OCI archive: ${OUT_FILE}"
echo "source image: ${SOURCE_IMAGE}"
echo "runtime: ${ENGINE}/${ARCHITECTURE}"
echo "archive annotation: registry.local/inference-runtime:bundled"
echo "image reference: ${REFERENCE}"
