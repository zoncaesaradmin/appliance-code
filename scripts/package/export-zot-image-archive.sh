#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-zot-image-archive.sh --out-file PATH [options]

Exports one pinned linux/amd64 Zot platform manifest as an OCI archive.
The archive annotation is registry.local/zot:bundled and the emitted workload
reference is registry.local/zot@sha256:<archive index manifest digest>.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --source-image REF        Upstream pinned source. Default:
                            ghcr.io/project-zot/zot-linux-amd64:<zot version>
  --zot-version VERSION     Compatibility version. Defaults to chart appVersion.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHART_YAML="${REPO_ROOT}/deploy/charts/appliance-registry/Chart.yaml"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
ZOT_VERSION=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --zot-version) ZOT_VERSION="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-zot-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-zot-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-zot-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${ZOT_VERSION}" ]]; then
  ZOT_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}")"
fi
if [[ -z "${SOURCE_IMAGE}" ]]; then
  SOURCE_IMAGE="ghcr.io/project-zot/zot-linux-amd64:${ZOT_VERSION}"
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

# Selecting linux/amd64 here makes index.json point at the platform manifest,
# never the upstream multi-architecture index.
skopeo copy --override-os linux --override-arch amd64 \
  "docker://${SOURCE_IMAGE}" "oci:${LAYOUT}:registry.local/zot:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/zot:bundled":
    raise SystemExit("OCI archive is missing registry.local/zot:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/zot@${DIGEST}"

rm -f "${OUT_FILE}"
tar -C "${LAYOUT}" -cf "${OUT_FILE}" .
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created Zot OCI archive: ${OUT_FILE}"
echo "archive annotation: registry.local/zot:bundled"
echo "image reference: ${REFERENCE}"
