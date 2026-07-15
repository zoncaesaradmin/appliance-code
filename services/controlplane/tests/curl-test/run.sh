#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUN_DIR="${ROOT_DIR}/.run/curl-test"
LOG_DIR="${RUN_DIR}/logs"
BIN_DIR="${ROOT_DIR}/bin"
SERVER_BIN="${BIN_DIR}/appliance-server"
DATA_DIR="${RUN_DIR}/data"
CONFIG_FILE="${RUN_DIR}/config.json"
PASSWORD_DIR="${RUN_DIR}/passwords"
PID_FILE="${RUN_DIR}/appliance-server.pid"
SERVER_LOG_FILE="${LOG_DIR}/appliance-server.log"
TEST_LOG_FILE="${LOG_DIR}/curl-test-output.log"
QUIET_MODE="${CURL_TEST_QUIET:-0}"

PUBLIC_ADDR="127.0.0.1:18082"
INTERNAL_ADDR="127.0.0.1:18083"
PUBLIC_URL="http://${PUBLIC_ADDR}"
INTERNAL_URL="http://${INTERNAL_ADDR}"

ADMIN_USERNAME="admin"
ADMIN_PASSWORD="admin-password-123"
ALICE_USERNAME="alice"
ALICE_PASSWORD="alice-password-123"
ROLE_NAME="curl-token-reader"
API_TOKEN_NAME="curl-admin-token"
ALICE_API_TOKEN_NAME="curl-alice-token"
BUILD_SOURCE_URL="https://git.internal.example.com/team/app"
BUILD_SOURCE_SHA="0123456789abcdef0123456789abcdef01234567"
BUILD_IMAGE_REPOSITORY="users/admin/app"
BUILD_IMAGE_TAG="v1"
BUILD_BUILDER_DIGEST="buildah@sha256:approved"

rm -rf "${RUN_DIR}"
mkdir -p "${RUN_DIR}" "${LOG_DIR}" "${BIN_DIR}" "${DATA_DIR}" "${PASSWORD_DIR}"
rm -f "${PID_FILE}"

cleanup() {
  if [[ -f "${PID_FILE}" ]]; then
    pid="$(cat "${PID_FILE}")"
    if kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" 2>/dev/null || true
      attempts=0
      while kill -0 "${pid}" 2>/dev/null; do
        attempts=$((attempts + 1))
        if [[ ${attempts} -ge 20 ]]; then
          kill -9 "${pid}" 2>/dev/null || true
          break
        fi
        sleep 1
      done
    fi
    rm -f "${PID_FILE}"
  fi
}

trap cleanup EXIT INT TERM

log_step() {
  if [[ "${QUIET_MODE}" != "1" ]]; then
    printf '%s\n' "$1"
  fi
}

fail() {
  printf '%s\n' "$1" >&2
  printf 'server log: %s\n' "${SERVER_LOG_FILE}" >&2
  printf 'curl test log: %s\n' "${TEST_LOG_FILE}" >&2
  if [[ -f "${SERVER_LOG_FILE}" ]]; then
    tail -n 50 "${SERVER_LOG_FILE}" >&2 || true
  fi
  exit 1
}

json_field() {
  local json="$1"
  local key="$2"
  printf '%s' "${json}" | tr -d '\n' | sed -n "s/.*\"${key}\":\"\\([^\"]*\\)\".*/\\1/p"
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if ! printf '%s' "${haystack}" | grep -Fq "${needle}"; then
    printf '%s did not contain expected text: %s\n' "${label}" "${needle}" >&2
    printf '%s\n' "${haystack}" >&2
    exit 1
  fi
}

assert_not_equal() {
  local left="$1"
  local right="$2"
  local label="$3"
  if [[ "${left}" == "${right}" ]]; then
    printf '%s unexpectedly matched: %s\n' "${label}" "${left}" >&2
    exit 1
  fi
}

curl_request() {
  local method="$1"
  local url="$2"
  local auth_header="${3:-}"
  local body_file="${4:-}"
  local content_type="${5:-application/json}"
  local quiet_stderr="${6:-0}"

  local tmp_body
  tmp_body="$(mktemp)"
  local -a args
  args=(-sS -X "${method}" -o "${tmp_body}" -w '%{http_code}')
  if [[ -n "${auth_header}" ]]; then
    args+=(-H "Authorization: ${auth_header}")
  fi
  if [[ -n "${body_file}" ]]; then
    args+=(-H "Content-Type: ${content_type}" --data-binary "@${body_file}")
  fi
  args+=("${url}")

  if [[ "${quiet_stderr}" == "1" ]]; then
    if ! CURL_RESPONSE_STATUS="$(curl "${args[@]}" 2>/dev/null)"; then
      CURL_RESPONSE_STATUS="000"
    fi
  elif ! CURL_RESPONSE_STATUS="$(curl "${args[@]}")"; then
    CURL_RESPONSE_STATUS="000"
  fi
  CURL_RESPONSE_BODY="$(cat "${tmp_body}")"
  rm -f "${tmp_body}"
}

curl_basic_request() {
  local method="$1"
  local url="$2"
  local basic_user="$3"
  local basic_password="$4"

  local tmp_body
  tmp_body="$(mktemp)"
  if ! CURL_RESPONSE_STATUS="$(curl -sS -X "${method}" -u "${basic_user}:${basic_password}" -o "${tmp_body}" -w '%{http_code}' "${url}")"; then
    CURL_RESPONSE_STATUS="000"
  fi
  CURL_RESPONSE_BODY="$(cat "${tmp_body}")"
  rm -f "${tmp_body}"
}

assert_status() {
  local expected="$1"
  local label="$2"
  if [[ "${CURL_RESPONSE_STATUS}" != "${expected}" ]]; then
    printf '%s returned HTTP %s, want %s\n' "${label}" "${CURL_RESPONSE_STATUS}" "${expected}" >&2
    printf '%s\n' "${CURL_RESPONSE_BODY}" >&2
    exit 1
  fi
}

wait_for_ready() {
  local attempts=0
  while true; do
    curl_request GET "${INTERNAL_URL}/health/ready" "" "" "application/json" "1"
    if [[ "${CURL_RESPONSE_STATUS}" == "200" ]]; then
      return
    fi
    attempts=$((attempts + 1))
    if [[ ${attempts} -ge 30 ]]; then
      fail "appliance-server did not become ready"
    fi
    sleep 1
  done
}

printf '%s\n' "${ADMIN_PASSWORD}" > "${PASSWORD_DIR}/admin.txt"
cat > "${CONFIG_FILE}" <<EOF
{
  "applianceProfile": "builder",
  "allowedGitSourceHosts": ["git.internal.example.com"],
  "allowedBuilderImageDigests": ["${BUILD_BUILDER_DIGEST}"],
  "buildCatalog": {
    "workProfiles": [
      {"name": "builder", "description": "Builder workflows"}
    ],
    "repos": [
      {"name": "app", "url": "${BUILD_SOURCE_URL}", "defaultRef": "${BUILD_SOURCE_SHA}"}
    ],
    "buildTargets": [
      {
        "name": "default",
        "aliases": ["app"],
        "workProfile": "builder",
        "repo": "app",
        "execution": "repo_script",
        "imageRepository": "${BUILD_IMAGE_REPOSITORY}",
        "imageTagTemplate": "{commit12}",
        "builderImageDigest": "${BUILD_BUILDER_DIGEST}"
      }
    ]
  }
}
EOF

log_step "Building appliance-server..."
(cd "${ROOT_DIR}" && go build -o "${SERVER_BIN}" ./cmd/appliance-server)

log_step "Bootstrapping initial administrator..."
(
  cd "${ROOT_DIR}"
  APPLIANCE_CONFIG_FILE="${CONFIG_FILE}" \
  APPLIANCE_DATA_DIR="${DATA_DIR}" \
  APPLIANCE_PUBLIC_ADDR="${PUBLIC_ADDR}" \
  APPLIANCE_INTERNAL_ADDR="${INTERNAL_ADDR}" \
  APPLIANCE_CANONICAL_ORIGIN="${PUBLIC_URL}" \
  "${SERVER_BIN}" bootstrap init --admin-username "${ADMIN_USERNAME}" --admin-password-file "${PASSWORD_DIR}/admin.txt"
)

log_step "Starting appliance-server on ${PUBLIC_URL}"
(
  cd "${ROOT_DIR}"
  APPLIANCE_CONFIG_FILE="${CONFIG_FILE}" \
  APPLIANCE_DATA_DIR="${DATA_DIR}" \
  APPLIANCE_PUBLIC_ADDR="${PUBLIC_ADDR}" \
  APPLIANCE_INTERNAL_ADDR="${INTERNAL_ADDR}" \
  APPLIANCE_CANONICAL_ORIGIN="${PUBLIC_URL}" \
  APPLIANCE_LOG_LEVEL="info" \
  nohup "${SERVER_BIN}" > "${SERVER_LOG_FILE}" 2>&1 &
  echo $! > "${PID_FILE}"
)

wait_for_ready

log_step "Running curl-based HTTP checks..."

curl_request GET "${INTERNAL_URL}/health/live"
assert_status 200 "GET /health/live"
assert_contains "${CURL_RESPONSE_BODY}" '"status":"ok"' "liveness response"

curl_request GET "${INTERNAL_URL}/health/startup"
assert_status 200 "GET /health/startup"
assert_contains "${CURL_RESPONSE_BODY}" '"status":"started"' "startup response"

curl_request GET "${INTERNAL_URL}/version"
assert_status 200 "GET /version"
assert_contains "${CURL_RESPONSE_BODY}" '"version"' "version response"

curl_request GET "${PUBLIC_URL}/api/v1/users"
assert_status 401 "GET /api/v1/users without auth"
assert_contains "${CURL_RESPONSE_BODY}" '"code":"unauthorized"' "unauthorized response"

login_body_file="${RUN_DIR}/login.json"
cat > "${login_body_file}" <<EOF
{"username":"${ADMIN_USERNAME}","password":"${ADMIN_PASSWORD}"}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/auth/login" "" "${login_body_file}"
assert_status 200 "POST /api/v1/auth/login"
assert_contains "${CURL_RESPONSE_BODY}" '"accessToken"' "login response"
admin_access_token="$(json_field "${CURL_RESPONSE_BODY}" "accessToken")"
admin_refresh_token="$(json_field "${CURL_RESPONSE_BODY}" "refreshToken")"

curl_request GET "${PUBLIC_URL}/api/v1/auth/session" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/auth/session"
assert_contains "${CURL_RESPONSE_BODY}" "\"userId\"" "session response"
assert_contains "${CURL_RESPONSE_BODY}" '"authMethod":"session"' "session response"

refresh_body_file="${RUN_DIR}/refresh.json"
cat > "${refresh_body_file}" <<EOF
{"refreshToken":"${admin_refresh_token}"}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/auth/refresh" "" "${refresh_body_file}"
assert_status 200 "POST /api/v1/auth/refresh"
new_admin_access_token="$(json_field "${CURL_RESPONSE_BODY}" "accessToken")"
assert_not_equal "${new_admin_access_token}" "${admin_access_token}" "refresh access token"
admin_access_token="${new_admin_access_token}"

create_token_body_file="${RUN_DIR}/create-token.json"
cat > "${create_token_body_file}" <<EOF
{"name":"${API_TOKEN_NAME}"}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/tokens" "Bearer ${admin_access_token}" "${create_token_body_file}"
assert_status 201 "POST /api/v1/tokens"
assert_contains "${CURL_RESPONSE_BODY}" "\"name\":\"${API_TOKEN_NAME}\"" "create token response"
admin_token_id="$(json_field "${CURL_RESPONSE_BODY}" "id")"
admin_api_token="$(json_field "${CURL_RESPONSE_BODY}" "token")"

curl_request GET "${PUBLIC_URL}/api/v1/tokens" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/tokens"
assert_contains "${CURL_RESPONSE_BODY}" "\"name\":\"${API_TOKEN_NAME}\"" "list tokens response"

create_user_body_file="${RUN_DIR}/create-user.json"
cat > "${create_user_body_file}" <<EOF
{"username":"${ALICE_USERNAME}","displayName":"Alice","password":"${ALICE_PASSWORD}"}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/users" "Bearer ${admin_access_token}" "${create_user_body_file}"
assert_status 201 "POST /api/v1/users"
assert_contains "${CURL_RESPONSE_BODY}" "\"username\":\"${ALICE_USERNAME}\"" "create user response"
alice_user_id="$(json_field "${CURL_RESPONSE_BODY}" "id")"

curl_request GET "${PUBLIC_URL}/api/v1/users" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/users"
assert_contains "${CURL_RESPONSE_BODY}" "\"username\":\"${ALICE_USERNAME}\"" "list users response"

curl_request GET "${PUBLIC_URL}/api/v1/roles" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/roles"
assert_contains "${CURL_RESPONSE_BODY}" '"name":"administrator"' "list roles response"

curl_request GET "${PUBLIC_URL}/api/v1/permissions" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/permissions"
assert_contains "${CURL_RESPONSE_BODY}" '"name":"users.read"' "list permissions response"

create_role_body_file="${RUN_DIR}/create-role.json"
cat > "${create_role_body_file}" <<EOF
{"name":"${ROLE_NAME}","permissions":["tokens.read.self","registry.pull"]}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/roles" "Bearer ${admin_access_token}" "${create_role_body_file}"
assert_status 201 "POST /api/v1/roles"
assert_contains "${CURL_RESPONSE_BODY}" "\"name\":\"${ROLE_NAME}\"" "create role response"
role_id="$(json_field "${CURL_RESPONSE_BODY}" "id")"

set_roles_body_file="${RUN_DIR}/set-user-roles.json"
cat > "${set_roles_body_file}" <<EOF
{"roleIds":["${role_id}"]}
EOF
curl_request PUT "${PUBLIC_URL}/api/v1/users/${alice_user_id}/roles" "Bearer ${admin_access_token}" "${set_roles_body_file}"
assert_status 204 "PUT /api/v1/users/{id}/roles"

alice_login_body_file="${RUN_DIR}/alice-login.json"
cat > "${alice_login_body_file}" <<EOF
{"username":"${ALICE_USERNAME}","password":"${ALICE_PASSWORD}"}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/auth/login" "" "${alice_login_body_file}"
assert_status 200 "POST /api/v1/auth/login for alice"
alice_access_token="$(json_field "${CURL_RESPONSE_BODY}" "accessToken")"

curl_request GET "${PUBLIC_URL}/api/v1/tokens" "Bearer ${alice_access_token}"
assert_status 200 "GET /api/v1/tokens for alice"

curl_request GET "${PUBLIC_URL}/api/v1/users" "Bearer ${alice_access_token}"
assert_status 403 "GET /api/v1/users for alice"
assert_contains "${CURL_RESPONSE_BODY}" '"code":"forbidden"' "alice forbidden response"

create_alice_token_body_file="${RUN_DIR}/create-alice-token.json"
cat > "${create_alice_token_body_file}" <<EOF
{"name":"${ALICE_API_TOKEN_NAME}","scopes":["registry.pull"]}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/users/${alice_user_id}/tokens" "Bearer ${admin_access_token}" "${create_alice_token_body_file}"
assert_status 201 "POST /api/v1/users/{id}/tokens"
alice_api_token="$(json_field "${CURL_RESPONSE_BODY}" "token")"

create_grant_body_file="${RUN_DIR}/create-grant.json"
cat > "${create_grant_body_file}" <<EOF
{"subjectType":"user","subjectId":"${alice_user_id}","pathPrefix":"ci/pipeline-a","actions":["pull"]}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/registry/grants" "Bearer ${admin_access_token}" "${create_grant_body_file}"
assert_status 201 "POST /api/v1/registry/grants"
grant_id="$(json_field "${CURL_RESPONSE_BODY}" "id")"

curl_request GET "${PUBLIC_URL}/api/v1/registry/grants" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/registry/grants"
assert_contains "${CURL_RESPONSE_BODY}" "\"id\":\"${grant_id}\"" "list registry grants response"

curl_basic_request GET "${PUBLIC_URL}/api/v1/registry/token?service=zot&scope=repository:ci/pipeline-a/app:pull" "${ALICE_USERNAME}" "${alice_api_token}"
assert_status 200 "GET /api/v1/registry/token"
assert_contains "${CURL_RESPONSE_BODY}" '"token"' "registry token response"

curl_basic_request GET "${PUBLIC_URL}/api/v1/registry/token?service=zot&scope=not-a-valid-scope" "${ALICE_USERNAME}" "${alice_api_token}"
assert_status 400 "GET /api/v1/registry/token with malformed scope"

delete_grant_url="${PUBLIC_URL}/api/v1/registry/grants/${grant_id}"
curl_request DELETE "${delete_grant_url}" "Bearer ${admin_access_token}"
assert_status 204 "DELETE /api/v1/registry/grants/{id}"

build_body_file="${RUN_DIR}/create-build.json"
cat > "${build_body_file}" <<EOF
{
  "sourceRepoUrl":"${BUILD_SOURCE_URL}",
  "sourceCommitSha":"${BUILD_SOURCE_SHA}",
  "imageRepository":"${BUILD_IMAGE_REPOSITORY}",
  "imageTag":"${BUILD_IMAGE_TAG}",
  "builderImageDigest":"${BUILD_BUILDER_DIGEST}"
}
EOF
curl_request POST "${PUBLIC_URL}/api/v1/builds" "Bearer ${admin_access_token}" "${build_body_file}"
assert_status 201 "POST /api/v1/builds"
build_id="$(json_field "${CURL_RESPONSE_BODY}" "id")"
assert_contains "${CURL_RESPONSE_BODY}" '"status":"running"' "create build response"

curl_request GET "${PUBLIC_URL}/api/v1/builds/${build_id}" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/builds/{id}"
assert_contains "${CURL_RESPONSE_BODY}" "\"id\":\"${build_id}\"" "get build response"

curl_request GET "${PUBLIC_URL}/api/v1/builds" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/builds"
assert_contains "${CURL_RESPONSE_BODY}" "\"id\":\"${build_id}\"" "list builds response"

curl_request GET "${PUBLIC_URL}/api/v1/builds/${build_id}/logs" "Bearer ${admin_access_token}"
assert_status 200 "GET /api/v1/builds/{id}/logs"
assert_contains "${CURL_RESPONSE_BODY}" "fake logs for workflow" "build logs response"

oversized_body_file="${RUN_DIR}/oversized-login.json"
{
  printf '{"username":"admin","password":"'
  awk 'BEGIN { for (i = 0; i < (1024 * 1024 + 64); i++) printf "x" }'
  printf '"}'
} > "${oversized_body_file}"
curl_request POST "${PUBLIC_URL}/api/v1/auth/login" "" "${oversized_body_file}"
assert_status 400 "POST /api/v1/auth/login oversized body"
assert_contains "${CURL_RESPONSE_BODY}" '"code":"validation_error"' "oversized login response"

if [[ -z "${admin_token_id}" ]]; then
  fail "could not locate admin API token id in create response"
fi

curl_request DELETE "${PUBLIC_URL}/api/v1/tokens/${admin_token_id}" "Bearer ${admin_access_token}"
assert_status 204 "DELETE /api/v1/tokens/{id}"

cat > "${TEST_LOG_FILE}" <<EOF
Server started
Public URL: ${PUBLIC_URL}
Internal URL: ${INTERNAL_URL}
Server log file: ${SERVER_LOG_FILE}
Curl test log file: ${TEST_LOG_FILE}
Data directory: ${DATA_DIR}

Validated flows:
- internal health and version
- unauthorized rejection
- login, session, refresh
- self token create/list/revoke
- user create/list
- roles and permissions list
- custom role create and assignment
- forbidden RBAC response for limited user
- registry grant CRUD
- registry token issuance
- build create/get/list/logs
- oversized JSON request handling
EOF

log_step "Curl-based HTTP checks passed."
