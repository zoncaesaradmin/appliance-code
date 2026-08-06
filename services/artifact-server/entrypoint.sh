#!/usr/bin/env bash
set -euo pipefail

# Host-visible artifact-server logs live under this path (hostPath mount).
# The process writes application.log via config.json log.output — no stdout tee.
SERVICE_LOG_DIR="/data/zon/logs/artifactserver"

mkdir -p "${SERVICE_LOG_DIR}"

printf '[%s] starting artifact-server\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

exec /usr/local/bin/artifact-server "$@"
