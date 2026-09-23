# Appliance chat UI integration plan

Status: compatibility/security spike in progress as of 2026-09-23. A pinned
source adaptation lives under `services/open-webui/`. Its slim image built in
the Linux `dev-build` container, and an egress-denied non-root/read-only smoke
test passed with `STATIC_DIR` on an explicit writable data volume. No image
has been adopted for release and no appliance runtime has changed. Browser
identity visibility, provider lockout, real inference streaming/WebSockets,
dependency review, and exact offline release-input closure remain gate items
before packaging or deployment implementation.

## Compatibility gate findings (Open WebUI v0.11.4)

The latest upstream release inspected for this gate is v0.11.4. Its
trusted-header integration uses email as the account key; the admin user list
renders `user.email` directly. That means the proposed synthetic identifier
would become a visible second account address, contrary to this plan's
appliance-only identity requirement. The chat frontend also passes the account
email into `{{USER_EMAIL}}` prompt variables, and its profile-image picker can
use the address with Gravatar. These paths must not disclose or transmit the
internal adapter key. The upstream Dockerfile defaults to
`UID=0`, `GID=0` and calls non-root configurations untested. This does not
prove it cannot run as a fixed non-root UID under a read-only root filesystem,
but it does mean the stock image cannot be declared compatible without a
runtime test. The Mac development host has no Docker/Podman/containerd CLI.
The Linux build host has the appliance `dev-build` tooling image, Podman, and
Buildah. The patched image passed the first non-root/read-only/egress-denied
startup test, but exact production PVC/proxy conditions still need validation.

Upstream permits UI extensions while retaining its required Open WebUI
branding; the appliance must not white-label or remove that branding without
meeting its license terms. The ordinary unbranded integration must not depend
on an online enterprise-license retrieval path. These are product and release
review items, not permission to bypass the offline invariant.

**Resolution required:** choose and validate a pinned, appliance-maintained
adaptation that removes the synthetic identity from every user-facing and
mutable account surface, prevents native identity/provider overrides, and
runs under the specified pod security context; or choose a different chat UI
that meets the contract. Do not replace this gate with a CSS hide, a mutable
username/email mapping, or a promise to test after release packaging. Repeat
the full gate against the exact resulting image digest before implementation
continues. The source review alone does not validate runtime behavior.

Evidence: [v0.11.4 Dockerfile](https://github.com/open-webui/open-webui/blob/v0.11.4/Dockerfile),
[v0.11.4 admin user list](https://github.com/open-webui/open-webui/blob/v0.11.4/src/lib/components/admin/Users/UserList.svelte),
[trusted-header behavior](https://docs.openwebui.com/features/authentication-access/auth/sso/),
[license/branding terms](https://docs.openwebui.com/license/), and
[offline license warning](https://docs.openwebui.com/deployment/kubernetes/).

## Outcome and product contract

Offer Open WebUI as the appliance's browser chat experience above the existing
OpenAI-compatible inference gateway. It is a separate, single-replica pod, on
by default whenever the selected profile enables inference. An appliance admin
can disable and re-enable it without deleting conversations. Users sign in
**only to the appliance**. Appliance identity, permissions, tokens, and logout
remain authoritative. Open WebUI must not ask for a separate username, email,
password, API key, or signup. Do not expose its account/key controls as an
alternative authentication path. The appliance's immutable user ID, not its
mutable username, display name, or a future SSO email, anchors each person's
Open WebUI account and chat history.

This is a chat front end, not a coding-agent runtime or model manager. A model's
coding-agent capability label continues to come from the inference manager;
Open WebUI does not change that classification. The Alpha appliance has one
enabled serving model at a time. If none is loaded, the chat entry point must
explain that state and direct an authorized admin to AI Services rather than
silently downloading or loading a model.

## Architecture

```text
browser -> appliance TLS/Traefik -> appliance-authenticated WebUI route
        -> Open WebUI Service (inference namespace, port 8080)
        -> inference-gateway.inference.svc.cluster.local:8080/v1
        -> on-demand inference-engine Service (Ollama or vLLM)

browser -> appliance UI/API -> control plane -> saved WebUI desired state
                                     -> reconcile WebUI Deployment replicas
```

No `host.docker.internal`, host port 8000, Docker daemon, second Ollama, GPU
request, NodePort, or direct pod exposure. Configure `ENABLE_OLLAMA_API=false`,
`ENABLE_OPENAI_API=true`, and `OPENAI_API_BASE_URL` to the in-cluster gateway.
Use `GET /v1/models` only for the currently served model; downloaded inventory
and model lifecycle stay in the existing AI Services API. Confirm that the
chosen Open WebUI release handles gateway `503`/no active engine, streaming,
model switches, and both engines without stale model selection.

### Browser URL

Prefer `https://<appliance-canonical-host>:3000/` served by a dedicated
Traefik HTTPS entry point with the existing appliance certificate and an
internal ClusterIP backend. The `3000` number preserves the familiar Docker
invocation's browser URL; it does **not** expose pod port 8080 directly.
Use the same canonical host so no new `.local` subdomain is needed. Add the
entry point, Service load-balancer/firewall/preflight checks, TLS route, and
upgrade/rollback handling in the owning repos. Do not serve the WebUI on
plaintext HTTP even when the appliance's optional `plaintext-http` capability
is enabled. Test this URL on both appliance FQDN and `.local` access, including
certificates, DNS, WebSockets, SSE, assets, redirects, and long responses.
If a dedicated entry point is not supportable under the supported K3s/Traefik
contract, record that finding before coding; do not silently switch to a
subpath or unauthenticated NodePort. A subpath requires its own asset,
WebSocket, and cookie compatibility proof.

## One-login authentication and identity

The current appliance SPA stores access/refresh credentials in browser
storage and sends access tokens as bearer headers. A top-level WebUI navigation
cannot reuse that bearer automatically. Existing Traefik ForwardAuth accepts
bearer credentials and recognizes only `/mcp` and `/v2`; it cannot be attached
to this route unchanged. The video-playback cookie is deliberately scoped to
video and must not be repurposed as a general WebUI credential.

1. Add an appliance-authorized launch API. The SPA calls it with the current
   bearer session (not an appliance API key). Only an active interactive
   principal with `inference.use` may launch. The response permits a short,
   one-time, audience-bound exchange to the WebUI origin. Transfer the code in
   a form POST body, not a URL/query string, browser history, local storage,
   referrer, or log. Bind it to the appliance session family, destination,
   origin, expiry, and nonce; reject replay. Perform CSRF/Origin checks.
2. At the WebUI origin, the exchange sets a dedicated opaque `Secure`,
   `HttpOnly`, `SameSite` browser cookie. The browser's WebUI traffic goes
   through an appliance-controlled authentication gateway (Traefik ForwardAuth
   plus an appliance endpoint, or a small narrowly scoped proxy). The gateway
   validates the opaque session server-side on **every** HTTP request and
   WebSocket handshake, re-checks user state and `inference.use`, and checks
   whether WebUI is enabled. Close existing WebSockets on revocation or on a
   bounded revalidation interval; checking only their initial handshake is
   insufficient. It returns 401/403/503 deliberately when those conditions
   fail. Do not use a long-lived shared bearer/API token in the
   WebUI pod. Cookie names and host/port scope must be analyzed: browsers do
   not isolate cookies by port. Strip the bridge cookie before forwarding to
   unrelated appliance services, never log it, and add CSRF protection for
   WebUI-origin mutations. Align expiry/renewal with the appliance session;
   appliance logout, session revocation, password change, user disablement,
   or permission removal must deny subsequent access. Test active WebSocket
   revocation as well as new requests.
3. The gateway strips **all** incoming trusted identity, role, and forwarding
   headers supplied by clients, then injects its own verified identity to
   Open WebUI. NetworkPolicy allows WebUI ingress only from the trusted proxy;
   no other pod or client may reach it directly. Never enable `WEBUI_AUTH=false`.
   Configure trusted-header authentication with the login form, local signup,
   local password auth, and Open WebUI API-key creation disabled. Prove there
   is no alternate direct login or backend-management path.
4. Open WebUI's trusted-header protocol currently requires an email-shaped
   value. Derive an opaque, non-routable, per-appliance identifier from the
   immutable appliance user ID (for example a versioned `u-<stable-id>` form
   in a reserved internal domain); send appliance display name separately.
   It is an adapter key, **not** a user email, credential, or source of truth.
   Verify actual upstream validation, first-user bootstrap, uniqueness,
   case-folding, chat ownership, account pages, exports, and upgrades. If the
   synthetic address appears as a user-managed email or can be used for
   password recovery/login, the integration does not meet the product
   contract: hide/disable those paths with a reviewed appliance adaptation or
   choose another UI. Do not ship a visible second identity merely because
   the header sign-in succeeds.
5. Map every appliance principal with `inference.use`, including appliance
   administrators, to a normal WebUI chat user; all others fail closed.
   Appliance administration remains in the appliance UI/API, not in native
   WebUI accounts. Neither a delegated `inference.admin` permission nor a
   guest/API token confers WebUI-wide administration. Prevent first-user
   bootstrap from creating a native WebUI admin. Scope or disable WebUI's own provider,
   model, tool, function, code-execution, terminal, external-connection,
   community-sharing, and API-key settings unless explicitly reviewed as
   appliance features. Prevent a WebUI admin setting from overriding the
   appliance's single gateway, enabled model, or authentication policy.

Open WebUI may retain an internal session/database account to associate chats,
but users neither create nor administer that identity. The appliance route
gate remains authoritative even if an old Open WebUI session cookie exists.
The adapter must use a stable signing key from a managed Secret, preserved
across restarts and restored with the data.

### Future appliance SSO

Keep the bridge's input as the appliance's stable principal ID and effective
permissions, not the current local password mechanism. Future enterprise
SSO/OIDC signs users **into the appliance** and links the external subject to
the existing appliance user ID. The WebUI bridge and chat-account key remain
unchanged, preserving history. Do not directly federate WebUI to an external
provider or switch its key to the provider's email. A native appliance OIDC
issuer can be considered later when several applications need a standard
protocol; it is not required for this feature. Document and test account
linking and migration before any future SSO cutover.

## Deployment, lifecycle, and API/UI

- Add an Open WebUI image to the inference-enabled release closure, a
  single-replica Deployment, ClusterIP Service, dedicated RWO PVC, Secret,
  probes, resource requests/limits, and restrictive NetworkPolicies. Pod
  ServiceAccount token is not mounted; WebUI needs **no** Kubernetes RBAC.
  Use a distinct fixed UID/GID, `fsGroup`, Restricted-compatible pod security,
  `readOnlyRootFilesystem`, and only explicit data, temporary/cache, and log
  mounts. Verify the selected upstream image under these constraints rather
  than assuming the Docker image is rootless-ready.
- The control plane owns an `enabled` desired-state setting (default `true`
  only when inference is enabled), stored durably. It reconciles Deployment
  replicas to 1 or 0 at startup and after a change, with status distinct from
  desired state (`starting`, `ready`, `stopping`, `disabled`, `failed`). Its
  Kubernetes Role is limited to reading and scaling the named WebUI Deployment
  in the inference namespace. A Helm upgrade must not inadvertently reset an
  admin-disabled instance to enabled; test reconciliation ordering, rollback,
  and restore. PVC and account data remain when disabled; no deletion or
  re-creation is the normal toggle mechanism.
- Add authenticated status and admin-toggle APIs: `GET` requires a suitable
  inference read permission; `PUT/PATCH` requires `inference.admin`, validation,
  an audit event, and idempotent behavior. Avoid exposing arbitrary replica
  counts or Kubernetes object mutation. Add OpenAPI, API tests, UI client,
  route mapping, redacted UI/control-plane logs, and an AI Services card with
  enabled toggle, progress/failure state, and `Open Chat UI` link. Hide/disable
  the link for unauthorized users while still enforcing server-side checks.
- On a clean install the WebUI pod starts without waiting for a model or
  internet. The manager starts independently and serves its cached catalog.
  A model must be enabled via the existing model lifecycle before chat works.
  WebUI should show an actionable no-model state, not a generic connection
  error. If the manager/engine restarts, conversations remain durable and the
  UI recovers without re-login or manual connection edits.

## Offline, supply chain, operations

- Select a specific reviewed Open WebUI version/image digest and verify image
  provenance, license/branding obligations, architecture, and vulnerability
  evidence. Do not use `ghcr.io/open-webui/open-webui:main` or a runtime pull.
  Export the pinned image into `appliance-code` release-input, include it in
  the signed `appliance-release` air-gap bundle, and update `appliance-ctl`
  bundle validation, preload/import/tag-to-digest, values injection, install,
  upgrade, rollback, and diagnostics. The image uses `registry.local/...@sha256`
  and `IfNotPresent` after verified preload. Follow the repository's full
  export -> release-input -> signed bundle -> preload -> Helm -> runtime path.
- Set `OFFLINE_MODE=true`, `HF_HUB_OFFLINE=1`, disable automatic
  embedding/reranking/Whisper updates, version checks, runtime pip installs,
  community sharing, and non-reviewed connectors. Deny WebUI public egress at
  the network boundary. Open WebUI's offline switch alone is not an egress
  firewall. Keep document/RAG, audio, code tools, web search, and embeddings
  off until their dependency closure is bundled and separately validated.
  Do not weaken the existing operator-directed model-acquisition window for
  the inference manager/engine.
- Use local/block-backed storage suitable for single-pod SQLite, not NFS/SMB;
  preserve the PVC, secret, and chosen bridge state in backup/restore and
  machine migration. Include ownership/writeability and disk-pressure checks
  in install/health/support diagnostics. Write functional flow logs under
  `/data/zon/logs/open-webui/` and sanitize auth, chat content, and secrets.
  Support the appliance's host-visible log and storage permission contract.
- Bound uploads/chat retention and WebUI CPU/memory/disk use separately from
  the inference model capacity calculation. Do not consume model-weight PVC
  space for chat history or turn on WebUI GPUs. Define what happens when the
  WebUI data volume is full or SQLite migration fails; preserve data and fail
  visibly, rather than reinstalling an empty database.

## Implementation sequence and release gates

1. **Compatibility/security spike:** verify the exact pinned Open WebUI image
   against non-root/read-only operation, trusted-header identity visibility,
   first-admin creation without local login, locked provider settings,
   streaming/WebSockets, both engine APIs, no-model behavior, and offline
   execution. Resolve any failures before cross-repo packaging work.
2. **Bundle and deployment:** implement the OCI producer/consumer contract,
   Helm resources, TLS route, network policies, PVC/Secret, and probes. Add
   chart/render and bundle/preload tests; test installed image resolution with
   public registry unreachable.
3. **Identity bridge:** implement one-time launch, cookie/session validation,
   header stripping/injection, role mapping, logout/revocation, and browser
   return-to-login. Security-test spoofed headers, alternate auth paths,
   replay, CSRF, cross-port cookies, disabled users, permission changes,
   and direct pod access.
4. **Day-2 UX:** durable enable/disable reconciliation, status/admin APIs,
   AI Services card, operator route mapping, redacted logs, and failure states.
   Verify default enabled, disable/re-enable persistence, no active model,
   model replacement, and engine restarts.
5. **End-to-end:** fresh install, public-egress-denied operation, upgrade,
   rollback, reboot, backup/restore, and machine migration on both `std-llm`
   and `acc-llm` where supported. Run `make verify` in every edited repository
   and check the signed bundle through browser chat success on a target.

Do not mark the feature complete from Helm rendering or a direct pod curl
alone. The done gate is an appliance user signing in once, opening chat without
another credential, sending and streaming a message through each supported
engine, retaining the same history across restart/upgrade/SSO-ready identity
mapping, and having an admin disable access without deleting that history.

## Source and existing-contract references

- Existing inference deployment and proxy: `deploy/charts/appliance-inference/`
  and `services/inference-manager/cmd/inference-manager/main.go`.
- Current browser auth and ForwardAuth policy: `services/controlplane-ui/src/auth.ts`,
  `services/controlplane/internal/httpapi/forwardauth.go`, and
  `services/controlplane/internal/forwardauth/policy.go`.
- Operator UI/API mapping: `docs/ui-control-plane-route-mapping.md`.
- Open WebUI: [Kubernetes deployment](https://docs.openwebui.com/deployment/kubernetes/),
  [environment configuration](https://docs.openwebui.com/reference/env-configuration/),
  [offline mode](https://docs.openwebui.com/tutorials/maintenance/offline-mode/),
  [SSO/trusted-header warnings](https://docs.openwebui.com/features/authentication-access/auth/sso/).
