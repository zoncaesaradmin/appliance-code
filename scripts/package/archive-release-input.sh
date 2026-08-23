#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
usage: archive-release-input.sh --out-file PATH --code-version VERSION --control-plane-image PATH --ui-image PATH --k3s-version VERSION [options]

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
  --ui-image PATH                  Appliance UI image archive. Required.
  --ui-image-reference REF         Canonical UI image reference contained in
                                   the OCI archive.
  --host-agent-image PATH          Pinned appliance host-agent OCI archive.
  --host-agent-image-reference REF
                                   Canonical
                                   registry.local/appliance-host-agent@sha256:...
                                   platform-manifest reference. Required.
  --host-agent-binary PATH         Host-side appliance host-agent daemon binary.
                                   Defaults to services/hostagent/bin/appliance-host-agentd.
  --host-packages-dir DIR          Offline host package directory to copy into
                                   release-input as host-packages/ (required for
                                   the complete product super-set).
                                   Layout must be OS/version/arch, for example
                                   ubuntu/24.04/amd64/*.deb.
  --host-packages-os-version VER   Ubuntu baseline expected under
                                   host-packages/ubuntu/<VER>/amd64/.
                                   Defaults to the OS_VERSION environment
                                   variable. Required for the complete product.
  --artifact-server-image PATH     Pinned artifact-server linux/amd64 OCI archive.
  --artifact-server-image-reference REF
                                   Canonical
                                   registry.local/artifact-server@sha256:...
                                   platform-manifest reference.
  --artifact-server-version VERSION
                                   artifact-server compatibility version.
                                   Defaults to the appliance-registry chart
                                   appVersion.
  --dns-image PATH                 Pinned CoreDNS linux/amd64 OCI archive.
  --dns-image-reference REF        Canonical registry.local/coredns@sha256:...
                                   platform-manifest reference.
  --dns-version VERSION            DNS compatibility version. Defaults to the
                                   appliance-dns chart appVersion.
  --inference-runtime-image PATH   Pinned inference-runtime linux/amd64 OCI archive.
  --inference-runtime-image-reference REF
                                   Canonical
                                   registry.local/inference-runtime@sha256:...
                                   platform-manifest reference.
  --inference-version VERSION      Inference compatibility version. Defaults to
                                   the appliance-inference chart appVersion.
  --video-runtime-image PATH       Pinned video-runtime linux/amd64 OCI archive.
  --video-runtime-image-reference REF
                                   Canonical
                                   registry.local/video-runtime@sha256:...
                                   platform-manifest reference.
  --video-version VERSION          Video compatibility version. Defaults to
                                   the appliance-video chart appVersion.
  --extra-oci-image PATH           Repeatable additional OCI image archive to
                                   include in release-input, for example a
                                   builder task image required by a profile.
  --extra-oci-image-reference REF  Repeatable canonical image reference for the
                                   corresponding --extra-oci-image.
  --workflows-version VERSION      Optional pinned workflows engine version.
  --workflow-controller-image PATH
                                   Optional workflow controller image archive.
  --workflow-controller-image-reference REF
                                   Canonical workflow controller image reference
                                   contained in the OCI archive.
  --workflow-executor-image PATH  Optional workflow executor image archive.
  --workflow-executor-image-reference REF
                                   Canonical workflow executor image reference
                                   contained in the OCI archive.
  --workflows-crds-dir DIR         Optional directory containing the versioned
                                   workflow CRD bundle to copy into release-input.
  --k3s-version VERSION            Pinned K3s version. Required.
  --chart-version VERSION          Chart version. Defaults to code version.
  --supported-upgrade-source VER   Repeatable. Adds a supported upgrade
                                   source version to compatibility metadata.
  --sbom-dir DIR                   Existing SBOM directory to copy.
  --provenance-dir DIR             Existing provenance directory to copy.
  --notices-dir DIR                Existing notices directory to copy.
  --tests-dir DIR                  Existing tests directory to copy.
  --metadata-bundle PATH             Appliance metadata-bundle archive
                                   (appliance-metadata-bundle-X.Y.Z.N.tar.zst).
                                   Defaults to generating from metadata-bundle/base.
  --message-broker-image PATH        Pinned NATS/JetStream OCI archive. Required.
  --message-broker-image-reference REF
                                   Canonical registry.local/nats@sha256:... reference.
  --help                           Show this help.
USAGE
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-control-plane"
MESSAGE_BROKER_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-message-broker"
WORKFLOWS_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-workflows"
ARTIFACT_SERVER_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-registry"
DNS_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-dns"
INFERENCE_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-inference"
VIDEO_CHART_DIR="${REPO_ROOT}/deploy/charts/appliance-video"
VALUES_SCHEMA_PATH="${CHART_DIR}/values.schema.json"

OUT_FILE=""
LATEST_OUT_FILE=""
CODE_VERSION=""
RELEASE_ID=""
CONTROL_PLANE_IMAGE=""
CONTROL_PLANE_IMAGE_REFERENCE=""
UI_IMAGE=""
UI_IMAGE_REFERENCE=""
HOST_AGENT_IMAGE=""
HOST_AGENT_IMAGE_REFERENCE=""
HOST_AGENT_BINARY=""
HOST_PACKAGES_DIR=""
HOST_PACKAGES_OS_VERSION="${OS_VERSION:-}"
ARTIFACT_SERVER_IMAGE=""
ARTIFACT_SERVER_IMAGE_REFERENCE=""
ARTIFACT_SERVER_VERSION=""
DNS_IMAGE=""
DNS_IMAGE_REFERENCE=""
DNS_VERSION=""
INFERENCE_RUNTIME_IMAGE=""
INFERENCE_RUNTIME_IMAGE_REFERENCE=""
INFERENCE_VERSION=""
VIDEO_RUNTIME_IMAGE=""
VIDEO_RUNTIME_IMAGE_REFERENCE=""
VIDEO_VERSION=""
WORKFLOWS_VERSION=""
WORKFLOW_CONTROLLER_IMAGE=""
WORKFLOW_CONTROLLER_IMAGE_REFERENCE=""
WORKFLOW_EXECUTOR_IMAGE=""
WORKFLOW_EXECUTOR_IMAGE_REFERENCE=""
WORKFLOWS_CRDS_DIR=""
EXTRA_OCI_IMAGES=()
EXTRA_OCI_IMAGE_REFERENCES=()
K3S_VERSION=""
CHART_VERSION=""
SBOM_DIR=""
PROVENANCE_DIR=""
NOTICES_DIR=""
TESTS_DIR=""
METADATA_BUNDLE=""
MESSAGE_BROKER_IMAGE=""
MESSAGE_BROKER_IMAGE_REFERENCE=""
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
    --ui-image)
      UI_IMAGE="${2:-}"
      shift 2
      ;;
    --ui-image-reference)
      UI_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --host-agent-image)
      HOST_AGENT_IMAGE="${2:-}"
      shift 2
      ;;
    --host-agent-image-reference)
      HOST_AGENT_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --host-agent-binary)
      HOST_AGENT_BINARY="${2:-}"
      shift 2
      ;;
    --host-packages-dir)
      HOST_PACKAGES_DIR="${2:-}"
      shift 2
      ;;
    --host-packages-os-version)
      HOST_PACKAGES_OS_VERSION="${2:-}"
      shift 2
      ;;
    --artifact-server-image)
      ARTIFACT_SERVER_IMAGE="${2:-}"
      shift 2
      ;;
    --artifact-server-image-reference)
      ARTIFACT_SERVER_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --artifact-server-version)
      ARTIFACT_SERVER_VERSION="${2:-}"
      shift 2
      ;;
    --dns-image)
      DNS_IMAGE="${2:-}"
      shift 2
      ;;
    --dns-image-reference)
      DNS_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --dns-version)
      DNS_VERSION="${2:-}"
      shift 2
      ;;
    --inference-runtime-image)
      INFERENCE_RUNTIME_IMAGE="${2:-}"
      shift 2
      ;;
    --inference-runtime-image-reference)
      INFERENCE_RUNTIME_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --inference-version)
      INFERENCE_VERSION="${2:-}"
      shift 2
      ;;
    --video-runtime-image)
      VIDEO_RUNTIME_IMAGE="${2:-}"
      shift 2
      ;;
    --video-runtime-image-reference)
      VIDEO_RUNTIME_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --video-version)
      VIDEO_VERSION="${2:-}"
      shift 2
      ;;
    --extra-oci-image)
      EXTRA_OCI_IMAGES+=("${2:-}")
      shift 2
      ;;
    --extra-oci-image-reference)
      EXTRA_OCI_IMAGE_REFERENCES+=("${2:-}")
      shift 2
      ;;
    --workflows-version)
      WORKFLOWS_VERSION="${2:-}"
      shift 2
      ;;
    --workflow-controller-image)
      WORKFLOW_CONTROLLER_IMAGE="${2:-}"
      shift 2
      ;;
    --workflow-controller-image-reference)
      WORKFLOW_CONTROLLER_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --workflow-executor-image)
      WORKFLOW_EXECUTOR_IMAGE="${2:-}"
      shift 2
      ;;
    --workflow-executor-image-reference)
      WORKFLOW_EXECUTOR_IMAGE_REFERENCE="${2:-}"
      shift 2
      ;;
    --workflows-crds-dir)
      WORKFLOWS_CRDS_DIR="${2:-}"
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
    --metadata-bundle)
      METADATA_BUNDLE="${2:-}"
      shift 2
      ;;
    --message-broker-image)
      MESSAGE_BROKER_IMAGE="${2:-}"
      shift 2
      ;;
    --message-broker-image-reference)
      MESSAGE_BROKER_IMAGE_REFERENCE="${2:-}"
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

bool_true() {
  local value="${1:-}"
  case "$(printf '%s' "${value}" | tr '[:upper:]' '[:lower:]')" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

if [[ -z "${HOST_AGENT_BINARY}" ]]; then
  HOST_AGENT_BINARY="${REPO_ROOT}/services/hostagent/bin/appliance-host-agentd"
fi
host_packages_required() {
  # Super-set packaging: host package payload is always part of the complete
  # product release-input. Install flags (host mDNS / Wi-Fi AP enablement) only
  # decide whether zonctl turns those packages on at install time.
  true
}

if [[ -z "${HOST_PACKAGES_DIR}" ]]; then
  HOST_PACKAGES_DIR="${REPO_ROOT}/.run/host-packages"
fi

require_host_packages_baseline() {
  local root="$1"
  local os_version="$2"
  local dir="${root}/ubuntu/${os_version}/amd64"
  local deb_count=""
  if [[ ! -d "${dir}" ]]; then
    echo "archive-release-input: host packages directory ${root} is missing ubuntu/${os_version}/amd64/*.deb" >&2
    exit 1
  fi
  deb_count="$(find "${dir}" -maxdepth 1 -type f -name '*.deb' | wc -l | tr -d '[:space:]')"
  if [[ "${deb_count}" == "0" ]]; then
    echo "archive-release-input: host packages directory ${root} is missing ubuntu/${os_version}/amd64/*.deb" >&2
    exit 1
  fi
}

if [[ -z "${OUT_FILE}" || -z "${CODE_VERSION}" || -z "${CONTROL_PLANE_IMAGE}" || -z "${UI_IMAGE}" || -z "${HOST_AGENT_IMAGE}" || -z "${HOST_AGENT_IMAGE_REFERENCE}" || -z "${K3S_VERSION}" ]]; then
  echo "archive-release-input: missing required arguments" >&2
  usage >&2
  exit 2
fi
if [[ -n "${MESSAGE_BROKER_IMAGE}" || -n "${MESSAGE_BROKER_IMAGE_REFERENCE}" ]]; then
  if [[ ! -f "${MESSAGE_BROKER_IMAGE}" ]]; then echo "archive-release-input: message broker image not found: ${MESSAGE_BROKER_IMAGE}" >&2; exit 1; fi
  if [[ ! "${MESSAGE_BROKER_IMAGE_REFERENCE}" =~ ^registry\.local/nats@sha256:[0-9a-f]{64}$ ]]; then echo "archive-release-input: --message-broker-image-reference must be registry.local/nats@sha256:<64 lowercase hex>" >&2; exit 2; fi
fi

if [[ ! -f "${CONTROL_PLANE_IMAGE}" ]]; then
  echo "archive-release-input: control-plane image not found: ${CONTROL_PLANE_IMAGE}" >&2
  exit 1
fi
if [[ ! -f "${UI_IMAGE}" ]]; then
  echo "archive-release-input: UI image not found: ${UI_IMAGE}" >&2
  exit 1
fi
if [[ ! -f "${HOST_AGENT_IMAGE}" ]]; then
  echo "archive-release-input: host-agent image not found: ${HOST_AGENT_IMAGE}" >&2
  exit 1
fi
if [[ ! -f "${HOST_AGENT_BINARY}" ]]; then
  echo "archive-release-input: host-agent binary not found: ${HOST_AGENT_BINARY}" >&2
  exit 1
fi
if [[ ! -d "${HOST_PACKAGES_DIR}" ]]; then
  echo "archive-release-input: host packages directory not found: ${HOST_PACKAGES_DIR} (complete product always packages host-packages)" >&2
  exit 1
fi
if [[ -z "${HOST_PACKAGES_OS_VERSION}" ]]; then
  echo "archive-release-input: --host-packages-os-version is required for host packages (or set OS_VERSION in the environment)" >&2
  exit 2
fi
require_host_packages_baseline "${HOST_PACKAGES_DIR}" "${HOST_PACKAGES_OS_VERSION}"
if [[ ! "${HOST_AGENT_IMAGE_REFERENCE}" =~ ^registry\.local/appliance-host-agent@sha256:[0-9a-f]{64}$ ]]; then
  echo "archive-release-input: --host-agent-image-reference must be registry.local/appliance-host-agent@sha256:<64 lowercase hex>" >&2
  exit 2
fi
python3 - "${HOST_AGENT_IMAGE}" "${HOST_AGENT_IMAGE_REFERENCE}" <<'PY'
import json, sys, tarfile

archive_path, expected_ref = sys.argv[1], sys.argv[2]
with tarfile.open(archive_path, "r:*") as tf:
    member = next((m for m in tf.getmembers() if m.name.lstrip("./") == "index.json"), None)
    if member is None:
        raise SystemExit("archive-release-input: host-agent OCI archive has no index.json")
    if not member.isfile():
        raise SystemExit("archive-release-input: host-agent OCI index.json is not a regular file")
    index = json.load(tf.extractfile(member))
manifests = index.get("manifests") or []
if len(manifests) != 1:
    raise SystemExit(f"archive-release-input: host-agent OCI index must contain one platform manifest, found {len(manifests)}")
descriptor = manifests[0]
annotation = (descriptor.get("annotations") or {}).get("org.opencontainers.image.ref.name")
if annotation != "registry.local/appliance-host-agent:bundled":
    raise SystemExit(f"archive-release-input: host-agent OCI annotation is {annotation!r}, want 'registry.local/appliance-host-agent:bundled'")
digest = descriptor.get("digest") or ""
expected_digest = expected_ref.rsplit("@", 1)[-1]
if digest != expected_digest:
    raise SystemExit(
        f"archive-release-input: host-agent imageReference digest {expected_digest} "
        f"does not match archive index.json digest {digest}"
    )
PY
if [[ -n "${ARTIFACT_SERVER_IMAGE}" && ! -f "${ARTIFACT_SERVER_IMAGE}" ]]; then
  echo "archive-release-input: artifact-server image not found: ${ARTIFACT_SERVER_IMAGE}" >&2
  exit 1
fi
if [[ -n "${ARTIFACT_SERVER_IMAGE}" || -n "${ARTIFACT_SERVER_IMAGE_REFERENCE}" ]]; then
  if [[ -z "${ARTIFACT_SERVER_IMAGE}" || -z "${ARTIFACT_SERVER_IMAGE_REFERENCE}" ]]; then
    echo "archive-release-input: --artifact-server-image and --artifact-server-image-reference must be provided together" >&2
    exit 2
  fi
  if [[ ! "${ARTIFACT_SERVER_IMAGE_REFERENCE}" =~ ^registry\.local/artifact-server@sha256:[0-9a-f]{64}$ ]]; then
    echo "archive-release-input: --artifact-server-image-reference must be registry.local/artifact-server@sha256:<64 lowercase hex>" >&2
    exit 2
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "archive-release-input: python3 is required to validate the artifact-server OCI archive contract" >&2
    exit 1
  fi
  python3 - "${ARTIFACT_SERVER_IMAGE}" "${ARTIFACT_SERVER_IMAGE_REFERENCE}" <<'PY'
import json
import sys
import tarfile

archive, reference = sys.argv[1:]
with tarfile.open(archive, "r:*") as tf:
    member = next((m for m in tf.getmembers() if m.name.lstrip("./") == "index.json"), None)
    if member is None:
        raise SystemExit("archive-release-input: artifact-server OCI archive has no index.json")
    stream = tf.extractfile(member)
    if stream is None:
        raise SystemExit("archive-release-input: artifact-server OCI index.json is not a regular file")
    index = json.load(stream)
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"archive-release-input: artifact-server OCI index must contain one platform manifest, found {len(manifests)}")
descriptor = manifests[0]
annotation = descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name")
if annotation != "registry.local/artifact-server:bundled":
    raise SystemExit(f"archive-release-input: artifact-server OCI annotation is {annotation!r}, want 'registry.local/artifact-server:bundled'")
digest = descriptor.get("digest", "")
if reference != "registry.local/artifact-server@" + digest:
    raise SystemExit(f"archive-release-input: artifact-server image reference {reference!r} does not match index digest {digest!r}")
PY
fi
if [[ ! -d "${ARTIFACT_SERVER_CHART_DIR}" ]]; then
  echo "archive-release-input: missing appliance-registry chart: ${ARTIFACT_SERVER_CHART_DIR}" >&2
  exit 1
fi
if [[ -n "${DNS_IMAGE}" && ! -f "${DNS_IMAGE}" ]]; then
  echo "archive-release-input: CoreDNS image not found: ${DNS_IMAGE}" >&2
  exit 1
fi
if [[ -n "${DNS_IMAGE}" || -n "${DNS_IMAGE_REFERENCE}" ]]; then
  if [[ -z "${DNS_IMAGE}" || -z "${DNS_IMAGE_REFERENCE}" ]]; then
    echo "archive-release-input: --dns-image and --dns-image-reference must be provided together" >&2
    exit 2
  fi
  if [[ ! "${DNS_IMAGE_REFERENCE}" =~ ^registry\.local/coredns@sha256:[0-9a-f]{64}$ ]]; then
    echo "archive-release-input: --dns-image-reference must be registry.local/coredns@sha256:<64 lowercase hex>" >&2
    exit 2
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "archive-release-input: python3 is required to validate the CoreDNS OCI archive contract" >&2
    exit 1
  fi
  python3 - "${DNS_IMAGE}" "${DNS_IMAGE_REFERENCE}" <<'PY'
import json
import sys
import tarfile

archive, reference = sys.argv[1:]
with tarfile.open(archive, "r:*") as tf:
    member = next((m for m in tf.getmembers() if m.name.lstrip("./") == "index.json"), None)
    if member is None:
        raise SystemExit("archive-release-input: CoreDNS OCI archive has no index.json")
    stream = tf.extractfile(member)
    if stream is None:
        raise SystemExit("archive-release-input: CoreDNS OCI index.json is not a regular file")
    index = json.load(stream)
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"archive-release-input: CoreDNS OCI index must contain one platform manifest, found {len(manifests)}")
descriptor = manifests[0]
annotation = descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name")
if annotation != "registry.local/coredns:bundled":
    raise SystemExit(f"archive-release-input: CoreDNS OCI annotation is {annotation!r}, want 'registry.local/coredns:bundled'")
digest = descriptor.get("digest", "")
if reference != "registry.local/coredns@" + digest:
    raise SystemExit(f"archive-release-input: CoreDNS image reference {reference!r} does not match index digest {digest!r}")
PY
fi
if [[ ! -d "${DNS_CHART_DIR}" ]]; then
  echo "archive-release-input: missing appliance-dns chart: ${DNS_CHART_DIR}" >&2
  exit 1
fi
if [[ -n "${INFERENCE_RUNTIME_IMAGE}" && ! -f "${INFERENCE_RUNTIME_IMAGE}" ]]; then
  echo "archive-release-input: inference-runtime image not found: ${INFERENCE_RUNTIME_IMAGE}" >&2
  exit 1
fi
if [[ -n "${INFERENCE_RUNTIME_IMAGE}" || -n "${INFERENCE_RUNTIME_IMAGE_REFERENCE}" || -n "${INFERENCE_VERSION}" ]]; then
  if [[ -z "${INFERENCE_RUNTIME_IMAGE}" || -z "${INFERENCE_RUNTIME_IMAGE_REFERENCE}" ]]; then
    echo "archive-release-input: --inference-runtime-image and --inference-runtime-image-reference must be provided together" >&2
    exit 2
  fi
  if [[ ! "${INFERENCE_RUNTIME_IMAGE_REFERENCE}" =~ ^registry\.local/inference-runtime@sha256:[0-9a-f]{64}$ ]]; then
    echo "archive-release-input: --inference-runtime-image-reference must be registry.local/inference-runtime@sha256:<64 lowercase hex>" >&2
    exit 2
  fi
  if [[ ! -d "${INFERENCE_CHART_DIR}" ]]; then
    echo "archive-release-input: missing appliance-inference chart: ${INFERENCE_CHART_DIR}" >&2
    exit 1
  fi
  if [[ -z "${INFERENCE_VERSION}" ]]; then
    INFERENCE_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${INFERENCE_CHART_DIR}/Chart.yaml")"
  fi
  # compatibility.inferenceVersion is unprefixed; Chart.yaml appVersion may be v0.6.5.
  INFERENCE_VERSION="${INFERENCE_VERSION#v}"
  if [[ -z "${INFERENCE_VERSION}" ]]; then
    echo "archive-release-input: unable to derive inferenceVersion from ${INFERENCE_CHART_DIR}/Chart.yaml" >&2
    exit 1
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "archive-release-input: python3 is required to validate the inference-runtime OCI archive contract" >&2
    exit 1
  fi
  python3 - "${INFERENCE_RUNTIME_IMAGE}" "${INFERENCE_RUNTIME_IMAGE_REFERENCE}" <<'PY'
import json
import sys
import tarfile

archive, reference = sys.argv[1:]
with tarfile.open(archive, "r:*") as tf:
    member = next((m for m in tf.getmembers() if m.name.lstrip("./") == "index.json"), None)
    if member is None:
        raise SystemExit("archive-release-input: inference-runtime OCI archive has no index.json")
    stream = tf.extractfile(member)
    if stream is None:
        raise SystemExit("archive-release-input: inference-runtime OCI index.json is not a regular file")
    index = json.load(stream)
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"archive-release-input: inference-runtime OCI index must contain one platform manifest, found {len(manifests)}")
descriptor = manifests[0]
annotation = descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name")
if annotation != "registry.local/inference-runtime:bundled":
    raise SystemExit(f"archive-release-input: inference-runtime OCI annotation is {annotation!r}, want 'registry.local/inference-runtime:bundled'")
digest = descriptor.get("digest", "")
if reference != "registry.local/inference-runtime@" + digest:
    raise SystemExit(f"archive-release-input: inference-runtime image reference {reference!r} does not match index digest {digest!r}")
PY
fi
# Pack-selective builds may omit inference inputs (inference pack not selected).
# Never ship inferenceVersion/chart without the runtime image.
if [[ -z "${INFERENCE_RUNTIME_IMAGE}" ]]; then
  INFERENCE_VERSION=""
fi
if [[ -n "${VIDEO_RUNTIME_IMAGE}" && ! -f "${VIDEO_RUNTIME_IMAGE}" ]]; then
  echo "archive-release-input: video-runtime image not found: ${VIDEO_RUNTIME_IMAGE}" >&2
  exit 1
fi
if [[ -n "${VIDEO_RUNTIME_IMAGE}" || -n "${VIDEO_RUNTIME_IMAGE_REFERENCE}" || -n "${VIDEO_VERSION}" ]]; then
  if [[ -z "${VIDEO_RUNTIME_IMAGE}" || -z "${VIDEO_RUNTIME_IMAGE_REFERENCE}" ]]; then
    echo "archive-release-input: --video-runtime-image and --video-runtime-image-reference must be provided together" >&2
    exit 2
  fi
  if [[ ! "${VIDEO_RUNTIME_IMAGE_REFERENCE}" =~ ^registry\.local/video-runtime@sha256:[0-9a-f]{64}$ ]]; then
    echo "archive-release-input: --video-runtime-image-reference must be registry.local/video-runtime@sha256:<64 lowercase hex>" >&2
    exit 2
  fi
  if [[ ! -d "${VIDEO_CHART_DIR}" ]]; then
    echo "archive-release-input: missing appliance-video chart: ${VIDEO_CHART_DIR}" >&2
    exit 1
  fi
  if [[ -z "${VIDEO_VERSION}" ]]; then
    VIDEO_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${VIDEO_CHART_DIR}/Chart.yaml")"
  fi
  # compatibility.videoVersion is unprefixed; Chart.yaml appVersion may be v10.10.7.
  VIDEO_VERSION="${VIDEO_VERSION#v}"
  if [[ -z "${VIDEO_VERSION}" ]]; then
    echo "archive-release-input: unable to derive videoVersion from ${VIDEO_CHART_DIR}/Chart.yaml" >&2
    exit 1
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "archive-release-input: python3 is required to validate the video-runtime OCI archive contract" >&2
    exit 1
  fi
  python3 - "${VIDEO_RUNTIME_IMAGE}" "${VIDEO_RUNTIME_IMAGE_REFERENCE}" <<'PY'
import json
import sys
import tarfile

archive, reference = sys.argv[1:]
with tarfile.open(archive, "r:*") as tf:
    member = next((m for m in tf.getmembers() if m.name.lstrip("./") == "index.json"), None)
    if member is None:
        raise SystemExit("archive-release-input: video-runtime OCI archive has no index.json")
    stream = tf.extractfile(member)
    if stream is None:
        raise SystemExit("archive-release-input: video-runtime OCI index.json is not a regular file")
    index = json.load(stream)
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"archive-release-input: video-runtime OCI index must contain one platform manifest, found {len(manifests)}")
descriptor = manifests[0]
annotation = descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name")
if annotation != "registry.local/video-runtime:bundled":
    raise SystemExit(f"archive-release-input: video-runtime OCI annotation is {annotation!r}, want 'registry.local/video-runtime:bundled'")
digest = descriptor.get("digest", "")
if reference != "registry.local/video-runtime@" + digest:
    raise SystemExit(f"archive-release-input: video-runtime image reference {reference!r} does not match index digest {digest!r}")
PY
fi
# Pack-selective builds may omit video inputs (video pack not selected).
# Never ship videoVersion/chart without the runtime image.
if [[ -z "${VIDEO_RUNTIME_IMAGE}" ]]; then
  VIDEO_VERSION=""
fi
if [[ -n "${WORKFLOW_CONTROLLER_IMAGE}" && ! -f "${WORKFLOW_CONTROLLER_IMAGE}" ]]; then
  echo "archive-release-input: workflow controller image not found: ${WORKFLOW_CONTROLLER_IMAGE}" >&2
  exit 1
fi
if [[ -n "${WORKFLOW_EXECUTOR_IMAGE}" && ! -f "${WORKFLOW_EXECUTOR_IMAGE}" ]]; then
  echo "archive-release-input: workflow executor image not found: ${WORKFLOW_EXECUTOR_IMAGE}" >&2
  exit 1
fi
if [[ ${#EXTRA_OCI_IMAGES[@]} -ne ${#EXTRA_OCI_IMAGE_REFERENCES[@]} ]]; then
  echo "archive-release-input: every --extra-oci-image must have a matching --extra-oci-image-reference" >&2
  exit 2
fi
if [[ ${#EXTRA_OCI_IMAGES[@]} -gt 0 ]]; then
  for extra_image in "${EXTRA_OCI_IMAGES[@]}"; do
    if [[ ! -f "${extra_image}" ]]; then
      echo "archive-release-input: extra OCI image not found: ${extra_image}" >&2
      exit 1
    fi
  done
  for extra_ref in "${EXTRA_OCI_IMAGE_REFERENCES[@]}"; do
    if [[ -z "${extra_ref}" ]]; then
      echo "archive-release-input: --extra-oci-image-reference must not be empty" >&2
      exit 2
    fi
  done
fi
if [[ -n "${WORKFLOWS_CRDS_DIR}" && ! -d "${WORKFLOWS_CRDS_DIR}" ]]; then
  echo "archive-release-input: workflows CRDs directory not found: ${WORKFLOWS_CRDS_DIR}" >&2
  exit 1
fi
# Package the workflows chart only when CRDs (or other workflows inputs) are
# provided. ADR 0011 still requires workflows in the complete appliance, but
# pack-selective builds (developer pack omitted) may skip workflows inputs.
# Never ship the chart without CRDs: that installs a controller that
# crash-loops on "get workflows.argoproj.io" until install times out.
if [[ -z "${WORKFLOWS_CRDS_DIR}" ]]; then
  if [[ -n "${WORKFLOWS_VERSION}" || -n "${WORKFLOW_CONTROLLER_IMAGE}" || -n "${WORKFLOW_EXECUTOR_IMAGE}" ]]; then
    echo "archive-release-input: workflows inputs require --workflows-crds-dir; the workflow controller cannot start without its CRDs" >&2
    exit 1
  fi
elif [[ ! -d "${WORKFLOWS_CHART_DIR}" ]]; then
  echo "archive-release-input: --workflows-crds-dir was set but workflows chart is missing: ${WORKFLOWS_CHART_DIR}" >&2
  exit 1
fi
if [[ ! -f "${VALUES_SCHEMA_PATH}" ]]; then
  echo "archive-release-input: missing chart values schema: ${VALUES_SCHEMA_PATH}" >&2
  exit 1
fi

if [[ -z "${CHART_VERSION}" ]]; then
  CHART_VERSION="${CODE_VERSION}"
fi
if [[ -z "${ARTIFACT_SERVER_VERSION}" ]]; then
  ARTIFACT_SERVER_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${ARTIFACT_SERVER_CHART_DIR}/Chart.yaml")"
fi
# compatibility.artifactServerVersion is unprefixed; Chart.yaml appVersion may be v2.1.8.
ARTIFACT_SERVER_VERSION="${ARTIFACT_SERVER_VERSION#v}"
if [[ -z "${ARTIFACT_SERVER_VERSION}" ]]; then
  echo "archive-release-input: unable to derive artifactServerVersion from ${ARTIFACT_SERVER_CHART_DIR}/Chart.yaml" >&2
  exit 1
fi
if [[ -z "${DNS_VERSION}" ]]; then
  DNS_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${DNS_CHART_DIR}/Chart.yaml")"
fi
# compatibility.dnsVersion is unprefixed; Chart.yaml appVersion may be v1.14.4.
DNS_VERSION="${DNS_VERSION#v}"
if [[ -z "${DNS_VERSION}" ]]; then
  echo "archive-release-input: unable to derive dnsVersion from ${DNS_CHART_DIR}/Chart.yaml" >&2
  exit 1
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
UI_BASENAME="$(basename "${UI_IMAGE}")"
HOST_AGENT_IMAGE_BASENAME="$(basename "${HOST_AGENT_IMAGE}")"
HOST_AGENT_BINARY_BASENAME="$(basename "${HOST_AGENT_BINARY}")"
ARTIFACT_SERVER_BASENAME=""
DNS_BASENAME=""
INFERENCE_RUNTIME_BASENAME=""
VIDEO_RUNTIME_BASENAME=""
CHART_ARCHIVE="appliance-chart-${CODE_VERSION}.tgz"
MESSAGE_BROKER_CHART_ARCHIVE="appliance-message-broker-${CODE_VERSION}.tgz"
WORKFLOWS_CHART_ARCHIVE="workflows-chart-${CODE_VERSION}.tgz"
ARTIFACT_SERVER_CHART_ARCHIVE="appliance-registry-chart-${CODE_VERSION}.tgz"
DNS_CHART_ARCHIVE="appliance-dns-chart-${CODE_VERSION}.tgz"
INFERENCE_CHART_ARCHIVE="appliance-inference-chart-${CODE_VERSION}.tgz"
VIDEO_CHART_ARCHIVE="appliance-video-chart-${CODE_VERSION}.tgz"
CONFIG_SCHEMA_BASENAME="configuration.schema.json"
COMPATIBILITY_BASENAME="compatibility.json"
CHECKSUMS_BASENAME="checksums.txt"

if [[ -z "${METADATA_BUNDLE}" ]]; then
  METADATA_BUNDLE="$("${SCRIPT_DIR}/generate-metadata-bundle.sh" --software-version "${CODE_VERSION}" --out-dir "${REPO_ROOT}/.run/metadata-bundle")"
fi
if [[ ! -f "${METADATA_BUNDLE}" ]]; then
  echo "archive-release-input: metadata bundle not found: ${METADATA_BUNDLE}" >&2
  exit 1
fi
METADATA_BUNDLE_BASENAME="$(basename "${METADATA_BUNDLE}")"
if [[ ! "${METADATA_BUNDLE_BASENAME}" =~ ^appliance-metadata-bundle-[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\.tar\.zst$ ]]; then
  echo "archive-release-input: metadata bundle basename must be appliance-metadata-bundle-X.Y.Z.N.tar.zst, got ${METADATA_BUNDLE_BASENAME}" >&2
  exit 1
fi
MESSAGE_BROKER_BASENAME=""
if [[ -n "${MESSAGE_BROKER_IMAGE}" ]]; then MESSAGE_BROKER_BASENAME="$(basename "${MESSAGE_BROKER_IMAGE}")"; fi

cp "${CONTROL_PLANE_IMAGE}" "${RELEASE_INPUT_DIR}/${CONTROL_PLANE_BASENAME}"
cp "${UI_IMAGE}" "${RELEASE_INPUT_DIR}/${UI_BASENAME}"
cp "${HOST_AGENT_IMAGE}" "${RELEASE_INPUT_DIR}/${HOST_AGENT_IMAGE_BASENAME}"
cp "${HOST_AGENT_BINARY}" "${RELEASE_INPUT_DIR}/${HOST_AGENT_BINARY_BASENAME}"
cp "${METADATA_BUNDLE}" "${RELEASE_INPUT_DIR}/${METADATA_BUNDLE_BASENAME}"
if [[ -n "${MESSAGE_BROKER_IMAGE}" ]]; then cp "${MESSAGE_BROKER_IMAGE}" "${RELEASE_INPUT_DIR}/${MESSAGE_BROKER_BASENAME}"; fi
copy_dir_or_empty "${HOST_PACKAGES_DIR}" "${RELEASE_INPUT_DIR}/host-packages"
if [[ -n "${ARTIFACT_SERVER_IMAGE}" ]]; then
  ARTIFACT_SERVER_BASENAME="$(basename "${ARTIFACT_SERVER_IMAGE}")"
  cp "${ARTIFACT_SERVER_IMAGE}" "${RELEASE_INPUT_DIR}/${ARTIFACT_SERVER_BASENAME}"
fi
if [[ -n "${DNS_IMAGE}" ]]; then
  DNS_BASENAME="$(basename "${DNS_IMAGE}")"
  cp "${DNS_IMAGE}" "${RELEASE_INPUT_DIR}/${DNS_BASENAME}"
fi
if [[ -n "${INFERENCE_RUNTIME_IMAGE}" ]]; then
  INFERENCE_RUNTIME_BASENAME="$(basename "${INFERENCE_RUNTIME_IMAGE}")"
  cp "${INFERENCE_RUNTIME_IMAGE}" "${RELEASE_INPUT_DIR}/${INFERENCE_RUNTIME_BASENAME}"
fi
if [[ -n "${VIDEO_RUNTIME_IMAGE}" ]]; then
  VIDEO_RUNTIME_BASENAME="$(basename "${VIDEO_RUNTIME_IMAGE}")"
  cp "${VIDEO_RUNTIME_IMAGE}" "${RELEASE_INPUT_DIR}/${VIDEO_RUNTIME_BASENAME}"
fi
cp "${VALUES_SCHEMA_PATH}" "${RELEASE_INPUT_DIR}/${CONFIG_SCHEMA_BASENAME}"

WORKFLOW_CONTROLLER_BASENAME=""
WORKFLOW_EXECUTOR_BASENAME=""
EXTRA_OCI_BASENAMES=()
if [[ -n "${WORKFLOW_CONTROLLER_IMAGE}" ]]; then
  WORKFLOW_CONTROLLER_BASENAME="$(basename "${WORKFLOW_CONTROLLER_IMAGE}")"
  cp "${WORKFLOW_CONTROLLER_IMAGE}" "${RELEASE_INPUT_DIR}/${WORKFLOW_CONTROLLER_BASENAME}"
fi
if [[ -n "${WORKFLOW_EXECUTOR_IMAGE}" ]]; then
  WORKFLOW_EXECUTOR_BASENAME="$(basename "${WORKFLOW_EXECUTOR_IMAGE}")"
  cp "${WORKFLOW_EXECUTOR_IMAGE}" "${RELEASE_INPUT_DIR}/${WORKFLOW_EXECUTOR_BASENAME}"
fi
if [[ ${#EXTRA_OCI_IMAGES[@]} -gt 0 ]]; then
  for extra_image in "${EXTRA_OCI_IMAGES[@]}"; do
    extra_basename="$(basename "${extra_image}")"
    EXTRA_OCI_BASENAMES+=("${extra_basename}")
    cp "${extra_image}" "${RELEASE_INPUT_DIR}/${extra_basename}"
  done
fi

mkdir -p "${TMP_DIR}/appliance-chart"
cp -R "${CHART_DIR}/." "${TMP_DIR}/appliance-chart/"
tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${CHART_ARCHIVE}" appliance-chart

mkdir -p "${TMP_DIR}/appliance-message-broker-chart"
cp -R "${MESSAGE_BROKER_CHART_DIR}/." "${TMP_DIR}/appliance-message-broker-chart/"
tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${MESSAGE_BROKER_CHART_ARCHIVE}" appliance-message-broker-chart

mkdir -p "${TMP_DIR}/appliance-registry-chart"
cp -R "${ARTIFACT_SERVER_CHART_DIR}/." "${TMP_DIR}/appliance-registry-chart/"
tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${ARTIFACT_SERVER_CHART_ARCHIVE}" appliance-registry-chart

mkdir -p "${TMP_DIR}/appliance-dns-chart"
cp -R "${DNS_CHART_DIR}/." "${TMP_DIR}/appliance-dns-chart/"
tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${DNS_CHART_ARCHIVE}" appliance-dns-chart

INFERENCE_CHART_BASENAME=""
if [[ -n "${INFERENCE_RUNTIME_IMAGE}" ]]; then
  mkdir -p "${TMP_DIR}/appliance-inference-chart"
  cp -R "${INFERENCE_CHART_DIR}/." "${TMP_DIR}/appliance-inference-chart/"
  tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${INFERENCE_CHART_ARCHIVE}" appliance-inference-chart
  INFERENCE_CHART_BASENAME="${INFERENCE_CHART_ARCHIVE}"
fi

VIDEO_CHART_BASENAME=""
if [[ -n "${VIDEO_RUNTIME_IMAGE}" ]]; then
  mkdir -p "${TMP_DIR}/appliance-video-chart"
  cp -R "${VIDEO_CHART_DIR}/." "${TMP_DIR}/appliance-video-chart/"
  tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${VIDEO_CHART_ARCHIVE}" appliance-video-chart
  VIDEO_CHART_BASENAME="${VIDEO_CHART_ARCHIVE}"
fi

if [[ -n "${WORKFLOWS_CRDS_DIR}" && -d "${WORKFLOWS_CHART_DIR}" ]]; then
  mkdir -p "${TMP_DIR}/workflows-chart"
  cp -R "${WORKFLOWS_CHART_DIR}/." "${TMP_DIR}/workflows-chart/"
  tar -C "${TMP_DIR}" -czf "${RELEASE_INPUT_DIR}/${WORKFLOWS_CHART_ARCHIVE}" workflows-chart
  copy_dir_or_empty "${WORKFLOWS_CRDS_DIR}" "${RELEASE_INPUT_DIR}/workflows-crds"
fi

{
  printf '{\n'
  printf '  "k3sVersion": "%s",\n' "${K3S_VERSION}"
  printf '  "chartVersion": "%s"' "${CHART_VERSION}"
  printf ',\n  "artifactServerVersion": "%s"' "${ARTIFACT_SERVER_VERSION}"
  printf ',\n  "dnsVersion": "%s"' "${DNS_VERSION}"
  if [[ -n "${INFERENCE_VERSION}" ]]; then
    printf ',\n  "inferenceVersion": "%s"' "${INFERENCE_VERSION}"
  fi
  if [[ -n "${VIDEO_VERSION}" ]]; then
    printf ',\n  "videoVersion": "%s"' "${VIDEO_VERSION}"
  fi
  if [[ -n "${WORKFLOWS_VERSION}" ]]; then
    printf ',\n  "workflowsVersion": "%s"' "${WORKFLOWS_VERSION}"
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
    "${UI_BASENAME}" \
    "${HOST_AGENT_IMAGE_BASENAME}" \
    "${HOST_AGENT_BINARY_BASENAME}" \
    "${CHART_ARCHIVE}" \
    "${ARTIFACT_SERVER_CHART_ARCHIVE}" \
    "${DNS_CHART_ARCHIVE}" \
    "${METADATA_BUNDLE_BASENAME}" \
    "${CONFIG_SCHEMA_BASENAME}" \
    "${COMPATIBILITY_BASENAME}"
  do
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${file}" | sed 's/^sha256://')" "${file}"
  done
  if [[ -f "${RELEASE_INPUT_DIR}/${WORKFLOWS_CHART_ARCHIVE}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${WORKFLOWS_CHART_ARCHIVE}" | sed 's/^sha256://')" "${WORKFLOWS_CHART_ARCHIVE}"
  fi
  if [[ -n "${ARTIFACT_SERVER_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${ARTIFACT_SERVER_BASENAME}" | sed 's/^sha256://')" "${ARTIFACT_SERVER_BASENAME}"
  fi
  if [[ -n "${DNS_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${DNS_BASENAME}" | sed 's/^sha256://')" "${DNS_BASENAME}"
  fi
  if [[ -n "${INFERENCE_CHART_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${INFERENCE_CHART_BASENAME}" | sed 's/^sha256://')" "${INFERENCE_CHART_BASENAME}"
  fi
  if [[ -n "${INFERENCE_RUNTIME_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${INFERENCE_RUNTIME_BASENAME}" | sed 's/^sha256://')" "${INFERENCE_RUNTIME_BASENAME}"
  fi
  if [[ -n "${VIDEO_CHART_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${VIDEO_CHART_BASENAME}" | sed 's/^sha256://')" "${VIDEO_CHART_BASENAME}"
  fi
  if [[ -n "${VIDEO_RUNTIME_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${VIDEO_RUNTIME_BASENAME}" | sed 's/^sha256://')" "${VIDEO_RUNTIME_BASENAME}"
  fi
  if [[ -n "${WORKFLOW_CONTROLLER_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${WORKFLOW_CONTROLLER_BASENAME}" | sed 's/^sha256://')" "${WORKFLOW_CONTROLLER_BASENAME}"
  fi
  if [[ -n "${WORKFLOW_EXECUTOR_BASENAME}" ]]; then
    printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${WORKFLOW_EXECUTOR_BASENAME}" | sed 's/^sha256://')" "${WORKFLOW_EXECUTOR_BASENAME}"
  fi
  if [[ ${#EXTRA_OCI_BASENAMES[@]} -gt 0 ]]; then
    for extra_basename in "${EXTRA_OCI_BASENAMES[@]}"; do
      printf '%s  %s\n' "$(sha256_file "${RELEASE_INPUT_DIR}/${extra_basename}" | sed 's/^sha256://')" "${extra_basename}"
    done
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

WORKFLOWS_COMPATIBILITY_JSON=""
if [[ -n "${WORKFLOWS_VERSION}" ]]; then
  WORKFLOWS_COMPATIBILITY_JSON=', "workflowsVersion": "'"${WORKFLOWS_VERSION}"'"'
fi
ARTIFACT_SERVER_COMPATIBILITY_JSON=', "artifactServerVersion": "'"${ARTIFACT_SERVER_VERSION}"'"'
DNS_COMPATIBILITY_JSON=', "dnsVersion": "'"${DNS_VERSION}"'"'
INFERENCE_COMPATIBILITY_JSON=""
if [[ -n "${INFERENCE_VERSION}" ]]; then
  INFERENCE_COMPATIBILITY_JSON=', "inferenceVersion": "'"${INFERENCE_VERSION}"'"'
fi
VIDEO_COMPATIBILITY_JSON=""
if [[ -n "${VIDEO_VERSION}" ]]; then
  VIDEO_COMPATIBILITY_JSON=', "videoVersion": "'"${VIDEO_VERSION}"'"'
fi

OPTIONAL_ARTIFACT_SERVER_IMAGE_JSON=""
if [[ -n "${ARTIFACT_SERVER_BASENAME}" ]]; then
  OPTIONAL_ARTIFACT_SERVER_IMAGE_JSON=',
    "artifactServerImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${ARTIFACT_SERVER_BASENAME}" "${ARTIFACT_SERVER_BASENAME}" "${ARTIFACT_SERVER_IMAGE_REFERENCE}")"
fi

OPTIONAL_DNS_IMAGE_JSON=""
if [[ -n "${DNS_BASENAME}" ]]; then
  OPTIONAL_DNS_IMAGE_JSON=',
    "dnsImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${DNS_BASENAME}" "${DNS_BASENAME}" "${DNS_IMAGE_REFERENCE}")"
fi

OPTIONAL_INFERENCE_ARTIFACTS_JSON=""
if [[ -n "${INFERENCE_CHART_BASENAME}" ]]; then
  OPTIONAL_INFERENCE_ARTIFACTS_JSON=',
    "inferenceChart": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${INFERENCE_CHART_BASENAME}" "${INFERENCE_CHART_BASENAME}")"
  if [[ -n "${INFERENCE_RUNTIME_BASENAME}" ]]; then
    OPTIONAL_INFERENCE_ARTIFACTS_JSON+=',
    "inferenceRuntimeImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${INFERENCE_RUNTIME_BASENAME}" "${INFERENCE_RUNTIME_BASENAME}" "${INFERENCE_RUNTIME_IMAGE_REFERENCE}")"
  fi
fi

OPTIONAL_VIDEO_ARTIFACTS_JSON=""
if [[ -n "${VIDEO_CHART_BASENAME}" ]]; then
  OPTIONAL_VIDEO_ARTIFACTS_JSON=',
    "videoChart": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${VIDEO_CHART_BASENAME}" "${VIDEO_CHART_BASENAME}")"
  if [[ -n "${VIDEO_RUNTIME_BASENAME}" ]]; then
    OPTIONAL_VIDEO_ARTIFACTS_JSON+=',
    "videoRuntimeImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${VIDEO_RUNTIME_BASENAME}" "${VIDEO_RUNTIME_BASENAME}" "${VIDEO_RUNTIME_IMAGE_REFERENCE}")"
  fi
fi

OPTIONAL_WORKFLOWS_ARTIFACTS_JSON=""
if [[ -f "${RELEASE_INPUT_DIR}/${WORKFLOWS_CHART_ARCHIVE}" ]]; then
  OPTIONAL_WORKFLOWS_ARTIFACTS_JSON+=',
    "workflowsChart": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${WORKFLOWS_CHART_ARCHIVE}" "${WORKFLOWS_CHART_ARCHIVE}")"
fi
if [[ -d "${RELEASE_INPUT_DIR}/workflows-crds" ]]; then
  OPTIONAL_WORKFLOWS_ARTIFACTS_JSON+=',
    "workflowsCRDs": '"$(render_dir_artifact "workflows-crds")"
fi
if [[ -n "${WORKFLOW_CONTROLLER_BASENAME}" ]]; then
  OPTIONAL_WORKFLOWS_ARTIFACTS_JSON+=',
    "workflowControllerImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${WORKFLOW_CONTROLLER_BASENAME}" "${WORKFLOW_CONTROLLER_BASENAME}" "${WORKFLOW_CONTROLLER_IMAGE_REFERENCE}")"
fi
if [[ -n "${WORKFLOW_EXECUTOR_BASENAME}" ]]; then
  OPTIONAL_WORKFLOWS_ARTIFACTS_JSON+=',
    "workflowExecutorImage": '"$(render_file_artifact "${RELEASE_INPUT_DIR}/${WORKFLOW_EXECUTOR_BASENAME}" "${WORKFLOW_EXECUTOR_BASENAME}" "${WORKFLOW_EXECUTOR_IMAGE_REFERENCE}")"
fi

OPTIONAL_EXTRA_OCI_IMAGES_JSON=""
if [[ ${#EXTRA_OCI_BASENAMES[@]} -gt 0 ]]; then
  OPTIONAL_EXTRA_OCI_IMAGES_JSON=',
    "extraOCIImages": ['
  for idx in "${!EXTRA_OCI_BASENAMES[@]}"; do
    if [[ ${idx} -gt 0 ]]; then
      OPTIONAL_EXTRA_OCI_IMAGES_JSON+=', '
    fi
    extra_basename="${EXTRA_OCI_BASENAMES[idx]}"
    extra_ref="${EXTRA_OCI_IMAGE_REFERENCES[idx]}"
    OPTIONAL_EXTRA_OCI_IMAGES_JSON+="$(render_file_artifact "${RELEASE_INPUT_DIR}/${extra_basename}" "${extra_basename}" "${extra_ref}")"
  done
  OPTIONAL_EXTRA_OCI_IMAGES_JSON+=']'
fi

HOST_PACKAGES_JSON=',
    "hostPackages": '"$(render_dir_artifact "host-packages")"

cat >"${RELEASE_INPUT_DIR}/release-input.json" <<JSON
{
  "schemaVersion": 1,
  "codeVersion": "${CODE_VERSION}",
  "releaseId": "${RELEASE_ID}",
  "generatedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "artifacts": {
    "controlPlaneImage": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CONTROL_PLANE_BASENAME}" "${CONTROL_PLANE_BASENAME}" "${CONTROL_PLANE_IMAGE_REFERENCE}"),
    "uiImage": $(render_file_artifact "${RELEASE_INPUT_DIR}/${UI_BASENAME}" "${UI_BASENAME}" "${UI_IMAGE_REFERENCE}"),
    "hostAgentImage": $(render_file_artifact "${RELEASE_INPUT_DIR}/${HOST_AGENT_IMAGE_BASENAME}" "${HOST_AGENT_IMAGE_BASENAME}" "${HOST_AGENT_IMAGE_REFERENCE}"),
    "hostAgentBinary": $(render_file_artifact "${RELEASE_INPUT_DIR}/${HOST_AGENT_BINARY_BASENAME}" "${HOST_AGENT_BINARY_BASENAME}")${HOST_PACKAGES_JSON},
    "applianceChart": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CHART_ARCHIVE}" "${CHART_ARCHIVE}"),
    "messageBrokerChart": $(render_file_artifact "${RELEASE_INPUT_DIR}/${MESSAGE_BROKER_CHART_ARCHIVE}" "${MESSAGE_BROKER_CHART_ARCHIVE}"),
    "artifactServerChart": $(render_file_artifact "${RELEASE_INPUT_DIR}/${ARTIFACT_SERVER_CHART_ARCHIVE}" "${ARTIFACT_SERVER_CHART_ARCHIVE}")${OPTIONAL_ARTIFACT_SERVER_IMAGE_JSON},
    "dnsChart": $(render_file_artifact "${RELEASE_INPUT_DIR}/${DNS_CHART_ARCHIVE}" "${DNS_CHART_ARCHIVE}")${OPTIONAL_DNS_IMAGE_JSON}${OPTIONAL_INFERENCE_ARTIFACTS_JSON}${OPTIONAL_VIDEO_ARTIFACTS_JSON},
    "metadataBundle": $(render_file_artifact "${RELEASE_INPUT_DIR}/${METADATA_BUNDLE_BASENAME}" "${METADATA_BUNDLE_BASENAME}"),
$(if [[ -n "${MESSAGE_BROKER_IMAGE}" ]]; then printf '    "messageBrokerImage": %s,\n' "$(render_file_artifact "${RELEASE_INPUT_DIR}/${MESSAGE_BROKER_BASENAME}" "${MESSAGE_BROKER_BASENAME}" "${MESSAGE_BROKER_IMAGE_REFERENCE}")"; fi)
    "configurationSchema": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CONFIG_SCHEMA_BASENAME}" "${CONFIG_SCHEMA_BASENAME}"),
    "compatibility": $(render_file_artifact "${RELEASE_INPUT_DIR}/${COMPATIBILITY_BASENAME}" "${COMPATIBILITY_BASENAME}"),
    "checksums": $(render_file_artifact "${RELEASE_INPUT_DIR}/${CHECKSUMS_BASENAME}" "${CHECKSUMS_BASENAME}"),
    "sbom": $(render_dir_artifact "sbom"),
    "provenance": $(render_dir_artifact "provenance"),
    "notices": $(render_dir_artifact "notices"),
    "tests": $(render_dir_artifact "tests")${OPTIONAL_WORKFLOWS_ARTIFACTS_JSON}${OPTIONAL_EXTRA_OCI_IMAGES_JSON}
  },
  "compatibility": {
    "k3sVersion": "${K3S_VERSION}",
    "chartVersion": "${CHART_VERSION}"${ARTIFACT_SERVER_COMPATIBILITY_JSON}${DNS_COMPATIBILITY_JSON}${INFERENCE_COMPATIBILITY_JSON}${VIDEO_COMPATIBILITY_JSON}${WORKFLOWS_COMPATIBILITY_JSON}${SUPPORTED_UPGRADES_JSON}
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
