#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-dns-server-image-archive.sh --out-file PATH [options]

Builds the appliance-owned DNS server wrapper image (upstream CoreDNS binary
+ log entrypoint that tees stdout/stderr into /data/zon/logs/dns) and exports
it as an OCI archive.

The archive annotation is registry.local/coredns:bundled and the emitted
workload reference is registry.local/coredns@sha256:<archive index digest>
(install OCI contract; upstream engine is CoreDNS).

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --source-image REF        Upstream CoreDNS image to wrap. Default:
                            registry.k8s.io/coredns/coredns:v<dns version>
  --dns-version VERSION     Compatibility version. Defaults to chart appVersion.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SERVICE_DIR="${REPO_ROOT}/services/dns-server"
CHART_YAML="${REPO_ROOT}/deploy/charts/appliance-dns/Chart.yaml"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
DNS_VERSION=""
LOCAL_IMAGE_PREFIX="localhost"
IMAGE_NAME="appliance-dns-server"
UPSTREAM_LOCAL_NAME="appliance-dns-server-upstream"
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
    --dns-version) DNS_VERSION="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-dns-server-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-dns-server-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in buildah skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-dns-server-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${DNS_VERSION}" ]]; then
  DNS_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}")"
fi
# Accept chart form v1.14.4 or compatibility form 1.14.4; registry.k8s.io tags use v.
DNS_VERSION="${DNS_VERSION#v}"
if [[ -z "${DNS_VERSION}" ]]; then
  echo "export-dns-server-image-archive: unable to derive dns version from ${CHART_YAML}" >&2
  exit 1
fi
IMAGE_TAG="v${DNS_VERSION}"
if [[ -z "${SOURCE_IMAGE}" ]]; then
  SOURCE_IMAGE="registry.k8s.io/coredns/coredns:v${DNS_VERSION}"
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
IMAGE_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"
UPSTREAM_LOCAL_REF="${LOCAL_IMAGE_PREFIX}/${UPSTREAM_LOCAL_NAME}:${IMAGE_TAG}"

# Prefetch linux/amd64 upstream into local storage so the wrapper build can
# use --pull-never (same pattern as the workflow controller wrapper).
retry "${PREFETCH_RETRIES}" \
  skopeo copy --override-os linux --override-arch amd64 \
    "docker://${SOURCE_IMAGE}" "containers-storage:${UPSTREAM_LOCAL_REF}"

make -C "${SERVICE_DIR}" image-local \
  BUILD_ENGINE="buildah bud --pull-never" \
  SERVICE_IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" \
  SERVICE_IMAGE_TAG="${IMAGE_TAG}" \
  BASE_IMAGE="${UPSTREAM_LOCAL_REF}" \
  RUNTIME_IMAGE="${RUNTIME_IMAGE:-}" \
  RUNTIME_PREBAKED="${RUNTIME_PREBAKED:-0}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

# Re-export the wrapper under the canonical :bundled annotation so install
# ValidateOCIArchiveReference / ctr import match the OCI contract.
skopeo copy --override-os linux --override-arch amd64 \
  "containers-storage:${IMAGE_REF}" "oci:${LAYOUT}:registry.local/coredns:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/coredns:bundled":
    raise SystemExit("OCI archive is missing registry.local/coredns:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/coredns@${DIGEST}"

rm -f "${OUT_FILE}"
# Pack explicit OCI layout members so the tar has index.json (not ./index.json).
# Python tarfile and zonctl readers look up the unprefixed name.
tar -C "${LAYOUT}" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created dns-server wrapper OCI archive: ${OUT_FILE}"
echo "wrapped upstream image: ${SOURCE_IMAGE}"
echo "archive annotation: registry.local/coredns:bundled"
echo "image reference: ${REFERENCE}"
