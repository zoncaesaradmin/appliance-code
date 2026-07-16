#!/usr/bin/env bash
set -euo pipefail

LOG_ROOT="/var/log/appliance"
SERVICE_LOG_DIR="${LOG_ROOT}/argo-controller"
STARTUP_LOG="${SERVICE_LOG_DIR}/startup.log"
STDOUT_LOG="${SERVICE_LOG_DIR}/stdout.log"
STDERR_LOG="${SERVICE_LOG_DIR}/stderr.log"

mkdir -p "${SERVICE_LOG_DIR}"
touch "${STARTUP_LOG}" "${STDOUT_LOG}" "${STDERR_LOG}"

# Mirror container stdout/stderr into predictable host log files while
# preserving the usual kubectl logs stream.
exec > >(tee -a "${STDOUT_LOG}") 2> >(tee -a "${STDERR_LOG}" >&2)

printf '[%s] starting workflow-controller\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"${STARTUP_LOG}"

exec /usr/local/bin/workflow-controller "$@"
