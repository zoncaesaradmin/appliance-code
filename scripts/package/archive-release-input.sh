#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
usage: archive-release-input.sh --out-file PATH --code-version VERSION --control-plane-image PATH --k3s-version VERSION [options]

Creates a versioned release-input tarball for appliance-release.

Options:
  --out-file PATH                  Output .tar.gz/.tgz file. Required.
  --latest-out-file PATH           Optional second path to copy the same tarball
                                   to, e.g. release-input-latest.tar.gz.
  --code-version VERSION           appliance-code version. Required.
  --release-id ID                  Release identifier. Defaults to
                                   local-<code-version>-<timestamp>.
  --control-plane-image PATH       Control-plane image archive. Required.
  --control-plane-image-reference REF
                                   Canonical control-plane image reference
                                   contained in the OCI archive.
  --argo-version VERSION           Optional pinned Argo Workflows version.
  --argo-controller-image PATH     Optional Argo controller image archive.
  --argo-controller-image-reference REF
                                   Canonical Argo controller image reference
                                   contained in the OCI archive.
  --argo-executor-image PATH       Optional Argo executor image archive.
  --argo-executor-image-reference REF
                                   Canonical Argo executor image reference
                                   contained in the OCI archive.
  --argo-crds-dir DIR              Optional directory containing the versioned
                                   Argo CRD bundle to copy into release-input.
  --k3s-version VERSION            Pinned K3s version. Required.
  --chart-version VERSION          Chart version. Defaults to code version.
  --supported-upgrade-source VER   Repeatable. Adds a supported upgrade
                                   source version to compatibility metadata.
  --sbom-dir DIR                   Existing SBOM directory to copy.
  --provenance-dir DIR             Existing provenance directory to copy.
  --notices-dir DIR                Existing notices directory to copy.
  --tests-dir DIR                  Existing tests directory to copy.
  --help                           Show this help.
USAGE
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-control-plane"
ARGO_CHART_DIR="${REPO_ROOT}/deploy/charts/argo-workflows"
VALUES_SCHEMA_PATH="${CHART_DIR}/values.schema.json"

OUT_FILE=""
LATEST_OUT_FILE=""
CODE_VERSION=""
RELEASE_ID=""
CONTROL_PLANE_IMAGE=""
CONTROL_PLANE_IMAGE_REFERENCE=""
ARGO_VERSION=""
ARGO_CONTROLLER_IMAGE=""
ARGO_CONTROLLER_IMAGE_REFERENCE=""
ARGO_EXECUTOR_IMAGE=""
ARGO_EXECUTOR_IMAGE_REFERENCE=""
ARGO_CRDS_DIR=""
K3S_VERSION=""
CHART_VERSION=""
SBOM_DIR=""
PROVENANCE_DIR=""
NOTICES_DIR=""
TESTS_DIR=""
SUPPORTED_UPGRADE_SOURCES=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file)
      OUT_FILE="${2:-}"
      shift 2
      ;;
    --latest-out-file)
      LATEST_OUT_FILE="${2:-}"
      shift 2
      ;;
    --code-version)
      CODE_VERSION="${2:-}"
      shift 2
      ;;
    --release-id)
      RELEASE_ID="${2:-}"
      shift 2
      ;;
    --control-plane-image)
      CONTROL_PLANE_IMAGE="${2:-}"
      shift 2
      ;;
    --control-plane-image-reference)
      CONTROL_PLANE_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --argo-version)
      ARGO_VERSION="${2:-}"
      shift 2
      ;;
    --argo-controller-image)
      ARGO_CONTROLLER_IMAGE="${2:-}"
      shift 2
      ;;
    --argo-controller-image-reference)
      ARGO_CONTROLLER_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --argo-executor-image)
      ARGO_EXECUTOR_IMAGE="${2:-}"
      shift 2
      ;;
    --argo-executor-image-reference)
      ARGO_EXECUTOR_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --argo-crds-dir)
      ARGO_CRDS_DIR="${2:-}"
      shift 2
      ;;
    --k3s-version)
      K3S_VERSION="${2:-}"
      shift 2
      ;;
    --chart-version)
      CHART_VERSION="${2:-}"
      shift 2
      ;;
    --supported-upgrade-source)
      SUPPORTED_UPGRADE_SOURCES+=("${2:-}")
      shift 2
      ;;
    --sbom-dir)
      SBOM_DIR="${2:-}"
      shift 2
      ;;
    --provenance-dir)
      PROVENANCE_DIR="${2:-}"
      shift 2
      ;;
    --notices-dir)
      NOTICES_DIR="${2:-}"
      shift 2
      ;;
    --tests-dir)
      TESTS_DIR="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "archive-release-input: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${OUT_FILE}" || -z "${CODE_VERSION}" || -z "${CONTROL_PLANE_IMAGE}" || -z "${K3S_VERSION}" ]]; then
  echo "archive-release-input: missing required arguments" >&2
  usage >&2
  exit 2
fi

if [[ ! -f "${CONTROL_PLANE_IMAGE}" ]]; then
  echo "archive-release-input: control-plane image not found: ${CONTROL_PLANE_IMAGE}" >&2
  exit 1
fi
if [[ -n "${ARGO_CONTROLLER_IMAGE}" && ! -f "${ARGO_CONTROLLER_IMAGE}" ]]; then
  echo "archive-release-input: Argo controller image not found: ${ARGO_CONTROLLER_IMAGE}" >&2
  exit 1
fi
if [[ -n "${ARGO_EXECUTOR_IMAGE}" && ! -f "${ARGO_EXECUTOR_IMAGE}" ]]; then
  echo "archive-release-input: Argo executor image not found: ${ARGO_EXECUTOR_IMAGE}" >&2
  exit 1
fi
if [[ -n "${ARGO_CRDS_DIR}" && ! -d "${ARGO_CRDS_DIR}" ]]; then
  echo "archive-release-input: Argo CRDs directory not found: ${ARGO_CRDS_DIR}" >&2
  exit 1
fi
if [[ ! -f "${VALUES_SCHEMA_PATH}" ]]; then
  echo "archive-release-input: missing chart values schema: ${VALUES_SCHEMA_PATH}" >&2
  exit 1
fi

if [[ -z "${CHART_VERSION}" ]]; then
  CHART_VERSION="${CODE_VERSION}"
fi
if [[ -z "${RELEASE_ID}" ]]; then
  RELEASE_ID="local-${CODE_VERSION}-$(date -u +%Y%m%dT%H%M%SZ)"
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
if [[ -n "${LATEST_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${LATEST_OUT_FILE}")"
  LATEST_OUT_FILE="$(cd "$(dirname "${LATEST_OUT_FILE}")" && pwd)/$(basename "${LATEST_OUT_FILE}")"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
RELEASE_INPUT_DIR="${TMP_DIR}/release-input"
mkdir -p "${RELEASE_INPUT_DIR}"

sha256_file() {
  local path="$1"
  local sum
  if command -v shasum >/dev/null 2>&1; then
    sum="$(shasum -a 256 "${path}" | awk '{print $1}')"
  else
    sum="$(sha256sum "${path}" | awk '{print $1}')"
  fi
  printf 'sha256:%s' "${sum}"
}

file_size() {
  local path="$1"
  if stat -f '%z' "${path}" >/dev/null 2>&1; then
    stat -f '%z' "${path}"
  else
    stat -c '%s' "${path}"
  fi
}

dir_manifest_digest() {
  local root="$1"
  local manifest=""
  while IFS= read -r file; do
    local rel digest size
    rel="${file#${root}/}"
    digest="$(sha256_file "${file}")"
    size="$(file_size "${file}")"
    manifest+="${rel}"$'\t'"${digest}"$'\t'"${size}"$'\n'
  done < <(find "${root}" -type f | LC_ALL=C sort)
  if command -v shasum >/dev/null 2>&1; then
    printf '%s' "${manifest}" | shasum -a 256 | awk '{print "sha256:" $1}'
  else
    printf '%s' "${manifest}" | sha256sum | awk '{print "sha256:" $1}'
  fi
}

copy_dir_or_empty() {
  local source="$1"
  local dest="$2"
  mkdir -p "${dest}"
  if [[ -n "${source}" ]]; then
    if [[ ! -d "${source}" ]]; then
      echo "archive-release-input: source directory not found: ${source}" >&2
      exit 1
    fi
    cp -R "${source}/." "${dest}/"
  fi
}

CONTROL_PLANE_BASENAME="$(basename "${CONTROL_PLANE_IMAGE}")"
CHART_ARCHIVE="appliance-chart-${CODE_VERSION}.tgz"
ARGO_CHART_ARCHIVE="argo-workflows-chart-${CODE_VERSION}.tgz"
CONFIG_SCHEMA_BASENAME="configuration.schema.json"
COMPATIBILITY_BASENAME="compatibility.json"
CHECKSUMS_BASENAME="checksums.txt"

cp "${CONTROL_PLANE_IMAGE}" "${RELEASE_INPUT_DIR}/${CONTROL_PLANE_BASENAME}"
cp "${VALUES_SCHEMA_PATH}" "${RELEASE_INPUT_DIR}/${CONFIG_SCHEMA_BASENAME}"

ARGO_CONTROLLER_BASENAME=""
ARGO_EXECUTOR_BASENAME=""
if [[ -n "${ARGO_CONTROLLER_IMAGE}" ]]; then
  ARGO_CONTROLLER_BASENAME="$(basename "${ARGO_CONTROLLER_IMAGE}")"
  cp "${ARGO_CONTROLLER_IMAGE}" "${RELEASE_INPUT_DIR}/${ARGO_CONTROLLER_BASENAME}"
fi
if [[ -n "${ARGO_EXECUTOR_IMAGE}" ]]; then
  ARGO_EXECUTOR_BASENAME="$(basename "${ARGO_EXECUTOR_IMAGE}")"
  cp "${ARGO_EXECUTOR_IMAGE}" "${RELEASE_INPUT_DIR}/${ARGO_EXECUTOR_BASENAME}"
fi

mkdir -p "${TMP_DIR}/appliance-chart"
cp -R "${CHART_DIR}/." "${TMP_DIR}/appliance-chart/"
tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${CHART_ARCHIVE}" appliance-chart

if [[ -d "${ARGO_CHART_DIR}" ]]; then
  mkdir -p "${TMP_DIR}/argo-workflows-chart"
  cp -R "${ARGO_CHART_DIR}/." "${TMP_DIR}/argo-workflows-chart/"
  tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${ARGO_CHART_ARCHIVE}" argo-workflows-chart
fi

if [[ -n "${ARGO_CRDS_DIR}" ]]; then
  copy_dir_or_empty "${ARGO_CRDS_DIR}" "${RELEASE_INPUT_DIR}/argo-crds"
fi

{
  printf '{\n'
  printf '  "k3sVersion": "%s",\n' "${K3S_VERSION}"
  printf '  "chartVersion": "%s"' "${CHART_VERSION}"
  if [[ -n "${ARGO_VERSION}" ]]; then
    printf ',\n  "argoVersion": "%s"' "${ARGO_VERSION}"
  fi
  if [[ ${#SUPPORTED_UPGRADE_SOURCES[@]} -gt 0 ]]; then
    printf ',\n  "supportedUpgradeSources": ['
    first=1
    for version in "${SUPPORTED_UPGRADE_SOURCES[@]}"; do
      if [[ ${first} -eq 0 ]]; then
        printf ', '
      fi
      first=0
      printf '"%s"' "${version}"
    done
    printf ']'
  fi
  printf '\n}\n'
} >"${RELEASE_INPUT_DIR}/${COMPATIBILITY_BASENAME}"

copy_dir_or_empty "${SBOM_DIR}" "${RELEASE_INPUT_DIR}/sbom"
copy_dir_or_empty "${PROVENANCE_DIR}" "${RELEASE_INPUT_DIR}/provenance"
copy_dir_or_empty "${NOTICES_DIR}" "${RELEASE_INPUT_DIR}/notices"
copy_dir_or_empty "${TESTS_DIR}" "${RELEASE_INPUT_DIR}/tests"

{
  for file in \
    "${CONTROL_PLANE_BASENAME}" \
    "${CHART_ARCHIVE}" \
    "${CONFIG_SCHEMA_BASENAME}" \
    "${COMPATIBILITY_BASENAME}"
  do
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${file}" | sed 's/^sha256://')" "${file}"
  done
  if [[ -f "${RELEASE_INPUT_DIR}/${ARGO_CHART_ARCHIVE}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${ARGO_CHART_ARCHIVE}" | sed 's/^sha256://')" "${ARGO_CHART_ARCHIVE}"
  fi
  if [[ -n "${ARGO_CONTROLLER_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${ARGO_CONTROLLER_BASENAME}" | sed 's/^sha256://')" "${ARGO_CONTROLLER_BASENAME}"
  fi
  if [[ -n "${ARGO_EXECUTOR_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${ARGO_EXECUTOR_BASENAME}" | sed 's/^sha256://')" "${ARGO_EXECUTOR_BASENAME}"
  fi
} >"${RELEASE_INPUT_DIR}/${CHECKSUMS_BASENAME}"

render_file_artifact() {
  local path="$1"
  local rel="$2"
  local image_reference="${3:-}"

  printf '{ "path": "%s", "digest": "%s", "sizeBytes": %s' \
    "${rel}" \
    "$(sha256_file "${path}")" \
    "$(file_size "${path}")"
  if [[ -n "${image_reference}" ]]; then
    printf ', "imageReference": "%s"' "${image_reference}"
  fi
  printf ' }'
}

render_dir_artifact() {
  local rel="$1"
  printf '{ "path": "%s", "manifestDigest": "%s" }' \
    "${rel}" \
    "$(dir_manifest_digest "${RELEASE_INPUT_DIR}/${rel}")"
}

SUPPORTED_UPGRADES_JSON=""
if [[ ${#SUPPORTED_UPGRADE_SOURCES[@]} -gt 0 ]]; then
  SUPPORTED_UPGRADES_JSON=', "supportedUpgradeSources": ['
  for idx in "${!SUPPORTED_UPGRADE_SOURCES[@]}"; do
    if [[ ${idx} -gt 0 ]]; then
      SUPPORTED_UPGRADES_JSON+=', '
    fi
    SUPPORTED_UPGRADES_JSON+="\"${SUPPORTED_UPGRADE_SOURCES[idx]}\""
  done
  SUPPORTED_UPGRADES_JSON+=']'
fi

ARGO_COMPATIBILITY_JSON=""
if [[ -n "${ARGO_VERSION}" ]]; then
  ARGO_COMPATIBILITY_JSON=', "argoVersion": "'"${ARGO_VERSION}"'"'
fi

OPTIONAL_ARGO_ARTIFACTS_JSON=""
if [[ -f "${RELEASE_INPUT_DIR}/${ARGO_CHART_ARCHIVE}" ]]; then
  OPTIONAL_ARGO_ARTIFACTS_JSON+=',
    "argoWorkflowsChart": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${ARGO_CHART_ARCHIVE}" "${ARGO_CHART_ARCHIVE}")"
fi
if [[ -d "${RELEASE_INPUT_DIR}/argo-crds" ]]; then
  OPTIONAL_ARGO_ARTIFACTS_JSON+=',
    "argoCRDs": '"$(render_dir_artifact "argo-crds")"
fi
if [[ -n "${ARGO_CONTROLLER_BASENAME}" ]]; then
  OPTIONAL_ARGO_ARTIFACTS_JSON+=',
    "argoControllerImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${ARGO_CONTROLLER_BASENAME}" "${ARGO_CONTROLLER_BASENAME}" "${ARGO_CONTROLLER_IMAGE_REFERENCE}")"
fi
if [[ -n "${ARGO_EXECUTOR_BASENAME}" ]]; then
  OPTIONAL_ARGO_ARTIFACTS_JSON+=',
    "argoExecutorImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${ARGO_EXECUTOR_BASENAME}" "${ARGO_EXECUTOR_BASENAME}" "${ARGO_EXECUTOR_IMAGE_REFERENCE}")"
fi

cat >"${RELEASE_INPUT_DIR}/release-input.json" <<JSON
{
  "schemaVersion": 1,
  "codeVersion": "${CODE_VERSION}",
  "releaseId": "${RELEASE_ID}",
  "generatedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "artifacts": {
    "controlPlaneImage": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CONTROL_PLANE_BASENAME}" "${CONTROL_PLANE_BASENAME}" "${CONTROL_PLANE_IMAGE_REFERENCE}"),
    "applianceChart": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CHART_ARCHIVE}" "${CHART_ARCHIVE}"),
    "configurationSchema": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CONFIG_SCHEMA_BASENAME}" "${CONFIG_SCHEMA_BASENAME}"),
    "compatibility": $(render_file_artifact "${RELEASE_INPUT_DIR}/${COMPATIBILITY_BASENAME}" "${COMPATIBILITY_BASENAME}"),
    "checksums": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CHECKSUMS_BASENAME}" "${CHECKSUMS_BASENAME}"),
    "sbom": $(render_dir_artifact "sbom"),
    "provenance": $(render_dir_artifact "provenance"),
    "notices": $(render_dir_artifact "notices"),
    "tests": $(render_dir_artifact "tests")${OPTIONAL_ARGO_ARTIFACTS_JSON}
  },
  "compatibility": {
    "k3sVersion": "${K3S_VERSION}",
    "chartVersion": "${CHART_VERSION}"${ARGO_COMPATIBILITY_JSON}${SUPPORTED_UPGRADES_JSON}
  }
}
JSON

tar -C "${RELEASE_INPUT_DIR}" -czf "${OUT_FILE}" .

if [[ -n "${LATEST_OUT_FILE}" ]]; then
  cp "${OUT_FILE}" "${LATEST_OUT_FILE}"
fi

echo "created release-input tarball:"
echo "  ${OUT_FILE}"
if [[ -n "${LATEST_OUT_FILE}" ]]; then
  echo "updated latest alias:"
  echo "  ${LATEST_OUT_FILE}"
fi
