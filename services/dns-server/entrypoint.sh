#!/usr/bin/env bash
set -euo pipefail

# Operator-visible DNS service logs (hostPath /data/zon/logs/dns).
# CoreDNS itself only writes to stdout/stderr; mirror those streams so
# host users can inspect query/error logs without kubectl.
#
# Non-root bind to :53 depends on zonctl hostdns.Prepare setting
# net.ipv4.ip_unprivileged_port_start=0 on the node (hostNetwork shares
# that sysctl). NET_BIND_SERVICE on the pod is belt-and-suspenders only;
# it does not survive this script's final exec into /coredns unless it is
# also ambient, which we do not raise here (capsh --addamb needs SETPCAP).
SERVICE_LOG_DIR="/data/zon/logs/dns"
STDOUT_LOG="${SERVICE_LOG_DIR}/stdout.log"
STDERR_LOG="${SERVICE_LOG_DIR}/stderr.log"

mkdir -p "${SERVICE_LOG_DIR}"
touch "${STDOUT_LOG}" "${STDERR_LOG}"
chmod 0644 "${STDOUT_LOG}" "${STDERR_LOG}"

# Mirror container stdout/stderr into predictable host log files while
# preserving the usual kubectl logs stream.
exec > >(tee -a "${STDOUT_LOG}") 2> >(tee -a "${STDERR_LOG}" >&2)

printf '[%s] starting dns-server\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

exec /coredns "$@"
