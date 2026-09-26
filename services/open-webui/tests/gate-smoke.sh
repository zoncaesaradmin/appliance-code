#!/usr/bin/env bash
# Run inside the appliance dev-build image, with the compatibility image in its
# local Podman store. This checks startup/auth under an egress-denied filesystem.
#
# Writable paths match the chart (data + cache + tmp) and are always tmpfs owned
# by UID/GID 10011. Named volumes with :U are not used: nested/rootless and
# cross-arch (qemu) packaging often leaves those mounts root-owned, which makes
# SQLite fail with "unable to open database file" for both arches.
set -euo pipefail

image="${1:?usage: gate-smoke.sh IMAGE_REF}"
suffix="${RANDOM}-$$"
container="zon-open-webui-gate-${suffix}"

cleanup() {
  podman rm -f "${container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

dump_gate_failure() {
  local reason="$1"
  echo "Open WebUI gate failure: ${reason}" >&2
  if podman container exists "${container}" 2>/dev/null; then
    echo "--- container inspect ---" >&2
    podman inspect "${container}" --format 'status={{.State.Status}} exit={{.State.ExitCode}} error={{.State.Error}} oom={{.State.OOMKilled}} arch={{.ImageArch}}' >&2 || true
    echo "--- container logs (tail 120) ---" >&2
    podman logs --tail 120 "${container}" >&2 || true
  else
    echo "container ${container} is gone (exited under --rm-equivalent cleanup or failed to start)" >&2
  fi
}

if podman image inspect "${image}" --format '{{range .Config.Env}}{{println .}}{{end}}' | \
  grep -q '^USE_SLIM_DOCKER=true$'; then
  :
else
  echo 'Open WebUI gate requires the appliance slim image' >&2
  exit 1
fi

# Run as the image's platform so same-arch and cross-arch freezes share one gate.
image_arch="$(podman image inspect "${image}" --format '{{.Architecture}}' 2>/dev/null || true)"
case "${image_arch}" in
  amd64|x86_64) image_arch=amd64 ;;
  arm64|aarch64) image_arch=arm64 ;;
  *)
    echo "Open WebUI gate: unsupported image architecture ${image_arch:-unknown}" >&2
    exit 1
    ;;
esac

# Do not use --rm: if the process exits we need logs. Trap removes the container.
# Runtime identity must match the image build UID/GID (10011) and chart securityContext.
# Writable paths are tmpfs with mode=1777 (world-writable + sticky). Do not use
# uid=/gid= mount options: several Podman/buildah versions reject them
# ("unknown mount option uid=…"). Named volumes with :U are also avoided —
# nested/rootless packaging often leaves those root-owned.
if ! podman run -d --name "${container}" --network=none --read-only \
  --arch "${image_arch}" \
  --user 10011:10011 --cap-drop=ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,mode=1777 \
  --tmpfs /app/backend/data:rw,mode=1777 \
  --tmpfs /app/.cache:rw,mode=1777 \
  -e HOME=/app \
  -e DATA_DIR=/app/backend/data \
  -e STATIC_DIR=/app/backend/data/static \
  -e DATABASE_URL=sqlite:////app/backend/data/webui.db \
  -e WEBUI_SECRET_KEY=test-only-not-release-secret \
  -e WEBUI_AUTH_TRUSTED_EMAIL_HEADER=X-Appliance-User \
  -e WEBUI_AUTH_TRUSTED_NAME_HEADER=X-Appliance-Name \
  -e WEBUI_AUTH_TRUSTED_ROLE_HEADER=X-Appliance-Role \
  -e DEFAULT_USER_ROLE=user \
  -e BYPASS_MODEL_ACCESS_CONTROL=true \
  -e ENABLE_PASSWORD_AUTH=false \
  -e ENABLE_SIGNUP=false \
  -e ENABLE_LOGIN_FORM=false \
  -e ENABLE_INITIAL_ADMIN_SIGNUP=false \
  -e ENABLE_PERSISTENT_CONFIG=false \
  -e OFFLINE_MODE=true \
  -e HF_HUB_OFFLINE=1 \
  -e ENABLE_OLLAMA_API=false \
  -e ENABLE_OPENAI_API=true \
  -e OPENAI_API_BASE_URL=http://127.0.0.1:9999/v1 \
  -e OPENAI_API_KEY=dummy \
  -e ENABLE_CODE_EXECUTION=false \
  -e ENABLE_MEMORIES=false \
  -e ENABLE_MEMORY_SYSTEM_CONTEXT=false \
  -e ENABLE_RETRIEVAL_QUERY_GENERATION=false \
  -e ENABLE_RAG_LOCAL_WEB_FETCH=false \
  -e ENABLE_DIRECT_CONNECTIONS=false \
  -e ENABLE_DIRECT_INTEGRATIONS=false \
  -e ENABLE_NOTES=false \
  -e ENABLE_CHANNELS=false \
  -e ENABLE_WEB_SEARCH=false \
  -e USER_PERMISSIONS_FEATURES_NOTES=false \
  -e USER_PERMISSIONS_FEATURES_CHANNELS=false \
  -e USER_PERMISSIONS_WORKSPACE_MODELS_ACCESS=false \
  -e USER_PERMISSIONS_WORKSPACE_KNOWLEDGE_ACCESS=false \
  -e USER_PERMISSIONS_WORKSPACE_PROMPTS_ACCESS=false \
  -e USER_PERMISSIONS_WORKSPACE_TOOLS_ACCESS=false \
  -e USER_PERMISSIONS_WORKSPACE_SKILLS_ACCESS=false \
  -e ENABLE_COMMUNITY_SHARING=false \
  -e ENABLE_API_KEYS=false \
  "${image}" >/dev/null; then
  dump_gate_failure "podman run failed"
  exit 1
fi

ready=false
for _ in $(seq 1 45); do
  status="$(podman inspect "${container}" --format '{{.State.Status}}' 2>/dev/null || true)"
  if [[ "${status}" != "running" ]]; then
    dump_gate_failure "container status=${status:-missing} before health ready"
    exit 1
  fi
  if podman exec "${container}" curl -fsS \
      --stderr /dev/null \
      http://127.0.0.1:8080/health 2>/dev/null | grep -q '"status":true'; then
    ready=true
    break
  fi
  sleep 2
done
if [[ "${ready}" != true ]]; then
  dump_gate_failure "health endpoint never became ready"
  exit 1
fi

signin_body='{"email":"unused@example.invalid","password":"unused"}'
signup_body='{"email":"local@example.invalid","password":"LocalPass123!","name":"Local"}'
signup_status="$(podman exec "${container}" curl -sS -o /dev/null -w '%{http_code}' \
  -H 'Content-Type: application/json' -d "${signup_body}" \
  http://127.0.0.1:8080/api/v1/auths/signup)"
[[ "${signup_status}" == 403 ]] || { echo "local signup: ${signup_status}" >&2; exit 1; }

missing_header_status="$(podman exec "${container}" curl -sS -o /dev/null -w '%{http_code}' \
  -H 'Content-Type: application/json' -d "${signin_body}" \
  http://127.0.0.1:8080/api/v1/auths/signin)"
[[ "${missing_header_status}" == 400 ]] || { echo "missing trusted header: ${missing_header_status}" >&2; exit 1; }

signin_response="$(podman exec "${container}" curl -fsS \
  -H 'Content-Type: application/json' \
  -H 'X-Appliance-User: u-gate@appliance.invalid' \
  -H 'X-Appliance-Name: Gate User' \
  -H 'X-Appliance-Role: user' \
  -d "${signin_body}" http://127.0.0.1:8080/api/v1/auths/signin)"
role="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["role"])' <<<"${signin_response}")"
[[ "${role}" == user ]] || { echo "trusted account role: ${role}" >&2; exit 1; }
token="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' <<<"${signin_response}")"
config_response="$(podman exec "${container}" curl -fsS \
  -H "Authorization: Bearer ${token}" http://127.0.0.1:8080/api/config)"
python3 -c 'import json,sys; f=json.load(sys.stdin)["features"]; assert f["auth_trusted_header"]; assert not any(f[k] for k in ("enable_direct_connections", "enable_channels", "enable_notes", "enable_code_execution", "enable_api_keys"))' <<<"${config_response}"
admin_status="$(podman exec "${container}" curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:8080/api/v1/auths/admin/config)"
[[ "${admin_status}" == 401 || "${admin_status}" == 403 ]] || {
  echo "native admin endpoint: ${admin_status}" >&2
  exit 1
}

profile_status="$(podman exec "${container}" curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
  -d '{"profile_image_url":"/user.png","name":"Different WebUI Name"}' \
  http://127.0.0.1:8080/api/v1/auths/update/profile)"
[[ "${profile_status}" == 403 ]] || { echo "native profile mutation: ${profile_status}" >&2; exit 1; }

if podman logs "${container}" 2>&1 | grep -E 'Read-only file system|Permission denied|unable to open database file' >/dev/null; then
  dump_gate_failure "read-only, permission, or sqlite error during startup"
  exit 1
fi

echo 'PASS: egress-denied non-root read-only startup and trusted-header auth'
