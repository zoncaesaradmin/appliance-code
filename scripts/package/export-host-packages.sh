#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
usage: export-host-packages.sh --out-dir DIR [options]

Stages the offline Ubuntu host package payload required by installer-owned
host capabilities such as mDNS.

Options:
  --out-dir DIR                  Destination root. Required. Packages are
                                 written under ubuntu/<osVersion>/<arch>/.
  --os-version VERSION           Ubuntu version to package. Defaults to the
                                 current host/container VERSION_ID.
  --arch ARCH                    Debian architecture. Default: amd64.
  --package NAME                 Repeatable root package to include.
                                 Defaults to avahi-daemon, avahi-utils,
                                 and libnss-mdns.
  --help                         Show this help.
USAGE
}

OUT_DIR=""
OS_VERSION=""
ARCH="amd64"
ROOT_PACKAGES=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    --os-version)
      OS_VERSION="${2:-}"
      shift 2
      ;;
    --arch)
      ARCH="${2:-}"
      shift 2
      ;;
    --package)
      ROOT_PACKAGES+=("${2:-}")
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "export-host-packages: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${OUT_DIR}" ]]; then
  echo "export-host-packages: --out-dir is required" >&2
  usage >&2
  exit 2
fi

for tool in apt-get dpkg; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "export-host-packages: ${tool} is required on PATH" >&2
    exit 1
  fi
done

if [[ ! -r /etc/os-release ]]; then
  echo "export-host-packages: /etc/os-release is required" >&2
  exit 1
fi

# shellcheck disable=SC1091
source /etc/os-release

if [[ "${ID:-}" != "ubuntu" ]]; then
  echo "export-host-packages: only Ubuntu packaging is supported, found ID=${ID:-<unknown>}" >&2
  exit 1
fi

CURRENT_OS_VERSION="${VERSION_ID:-}"
if [[ -z "${CURRENT_OS_VERSION}" ]]; then
  echo "export-host-packages: unable to derive VERSION_ID from /etc/os-release" >&2
  exit 1
fi
if [[ -z "${OS_VERSION}" ]]; then
  OS_VERSION="${CURRENT_OS_VERSION}"
fi
if [[ "${OS_VERSION}" != "${CURRENT_OS_VERSION}" ]]; then
  echo "export-host-packages: requested Ubuntu ${OS_VERSION}, but the current packaging environment is Ubuntu ${CURRENT_OS_VERSION}; use a matching Ubuntu build environment or provide a prebuilt host package override" >&2
  exit 1
fi
if [[ "${ARCH}" != "amd64" ]]; then
  echo "export-host-packages: only amd64 is supported, got ${ARCH}" >&2
  exit 1
fi

if [[ ${#ROOT_PACKAGES[@]} -eq 0 ]]; then
  ROOT_PACKAGES=(avahi-daemon avahi-utils libnss-mdns)
fi

OUT_DIR="$(cd "$(dirname "${OUT_DIR}")" && pwd)/$(basename "${OUT_DIR}")"
TARGET_DIR="${OUT_DIR}/ubuntu/${OS_VERSION}/${ARCH}"
TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

STATUS_DIR="${TMP_DIR}/state"
CACHE_DIR="${TMP_DIR}/cache"
mkdir -p "${STATUS_DIR}/lists/partial" "${CACHE_DIR}/archives/partial"
: > "${STATUS_DIR}/status"

APT_ARGS=(
  -o "Dir::State=${STATUS_DIR}"
  -o "Dir::State::status=${STATUS_DIR}/status"
  -o "Dir::Cache=${CACHE_DIR}"
  -o "Dir::Cache::archives=${CACHE_DIR}/archives"
  -o "Dir::Etc::sourcelist=/etc/apt/sources.list"
  -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d"
  -o "APT::Architecture=${ARCH}"
  -o "Acquire::Languages=none"
  -o "Debug::NoLocking=1"
)

echo "export-host-packages: refreshing apt metadata for Ubuntu ${OS_VERSION} (${ARCH})"
apt-get "${APT_ARGS[@]}" update -qq

echo "export-host-packages: downloading package closure for: ${ROOT_PACKAGES[*]}"
apt-get "${APT_ARGS[@]}" -y --download-only install "${ROOT_PACKAGES[@]}"

rm -rf "${TARGET_DIR}"
mkdir -p "${TARGET_DIR}"
find "${CACHE_DIR}/archives" -maxdepth 1 -type f -name '*.deb' -exec cp {} "${TARGET_DIR}/" \;

deb_count="$(find "${TARGET_DIR}" -maxdepth 1 -type f -name '*.deb' | wc -l | tr -d '[:space:]')"
if [[ "${deb_count}" == "0" ]]; then
  echo "export-host-packages: no .deb archives were downloaded into ${TARGET_DIR}" >&2
  exit 1
fi

echo "created host package payload:"
echo "  ${TARGET_DIR}"
echo "downloaded packages:"
find "${TARGET_DIR}" -maxdepth 1 -type f -name '*.deb' -printf '  %f\n' | sort
