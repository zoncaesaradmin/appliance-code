#!/usr/bin/env bash
set -euo pipefail

# Operator-visible DNS service logs (hostPath /data/zon/logs/dns).
# CoreDNS itself only writes to stdout/stderr; mirror those streams so
# host users can inspect query/error logs without kubectl.
#
# This script is invoked under capsh with CAP_NET_BIND_SERVICE already in
# the ambient set (see Containerfile ENTRYPOINT). Ambient caps survive the
# final exec into /coredns; a bare `exec /coredns` without ambient clears
# effective NET_BIND_SERVICE and non-root bind to :53 fails.
SERVICE_LOG_DIR="/data/zon/logs/dns"
STDOUT_LOG="${SERVICE_LOG_DIR}/stdout.log"
STDERR_LOG="${SERVICE_LOG_DIR}/stderr.log"

mkdir -p "${SERVICE_LOG_DIR}"
touch "${STDOUT_LOG}" "${STDERR_LOG}"
chmod 0644 "${STDOUT_LOG}" "${STDERR_LOG}"

# Mirror container stdout/stderr into predictable host log files while
# preserving the usual kubectl logs stream.
exec > >(tee -a "${STDOUT_LOG}") 2> >(tee -a "${STDERR_LOG}" >&2)

printf '[%s] starting coredns\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

exec /coredns "$@"
