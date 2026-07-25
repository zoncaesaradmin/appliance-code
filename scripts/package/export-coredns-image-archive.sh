#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-coredns-image-archive.sh --out-file PATH [options]

Exports one pinned linux/amd64 CoreDNS platform manifest as an OCI archive.
The archive annotation is registry.local/coredns:bundled and the emitted
workload reference is registry.local/coredns@sha256:<archive index digest>.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --source-image REF        Upstream pinned source. Default:
                            registry.k8s.io/coredns/coredns:<dns version with v>
  --dns-version VERSION     Compatibility version. Defaults to chart appVersion.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHART_YAML="${REPO_ROOT}/deploy/charts/appliance-dns/Chart.yaml"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
DNS_VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --dns-version) DNS_VERSION="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-coredns-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-coredns-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-coredns-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${DNS_VERSION}" ]]; then
  DNS_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}")"
fi
# Accept chart form v1.14.4 or compatibility form 1.14.4; registry.k8s.io tags use v.
DNS_VERSION="${DNS_VERSION#v}"
if [[ -z "${DNS_VERSION}" ]]; then
  echo "export-coredns-image-archive: unable to derive dns version from ${CHART_YAML}" >&2
  exit 1
fi
if [[ -z "${SOURCE_IMAGE}" ]]; then
  SOURCE_IMAGE="registry.k8s.io/coredns/coredns:v${DNS_VERSION}"
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

# Selecting linux/amd64 here makes index.json point at the platform manifest,
# never the upstream multi-architecture index.
skopeo copy --override-os linux --override-arch amd64 \
  "docker://${SOURCE_IMAGE}" "oci:${LAYOUT}:registry.local/coredns:bundled"

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
tar -C "${LAYOUT}" -cf "${OUT_FILE}" .
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created CoreDNS OCI archive: ${OUT_FILE}"
echo "archive annotation: registry.local/coredns:bundled"
echo "image reference: ${REFERENCE}"
