# Open WebUI implementation sequence

Open WebUI is an optional inference-pack component. It is never built,
exported, preloaded, deployed, or referenced by `foundation`.

## Pack contract

| Resolved pack | Open WebUI |
| --- | --- |
| `foundation` | absent |
| `std-llm` | included |
| `acc-llm` | included |

The release resolver, offline seed, release-input archive, installer preload,
and Helm deployment must all use the same condition: `std-llm || acc-llm`.
No profile, UI toggle, or host condition may cause foundation to acquire the
artifact.

## Remaining end-to-end work

The foundation work (source lock, patched-image exporter, optional chart
workloads, release-input fields, and durable one-time grant storage) exists.
The following is the complete remaining implementation checklist; no item may
be treated as optional when exposing the feature.

1. **Image supply chain.** Add the shared online/offline release dependency
   path in `appliance-release/deps/open-webui`: online obtains the exact
   `source.lock` revision; offline consumes only the LAN-seeded equivalent.
   Build the patched slim image and a gateway image for both supported target
   architectures. Export both only for `std-llm` / `acc-llm`, never foundation.
2. **Compatibility gate.** Run `tests/gate-smoke.sh` against the exact exported
   digest, then extend it for the fixed inference-manager OpenAI provider,
   streamed responses, WebSockets, restart persistence, and egress denial.
   A failed gate blocks package inclusion.
3. **Control-plane grant API.** Wire the durable grant store/service into
   `POST /api/v1/webui/launch`. It must require an interactive session (never
   an API token), active user, live session family, `inference.use`, a ready
   WebUI deployment, and a loaded model. It returns a raw, one-time,
   short-lived grant with no credential in logs, audit details, URLs, or
   browser storage.
4. **Gateway.** Implement and package the session-bridge gateway. Its launch
   endpoint consumes the grant exactly once, sets an opaque `Secure`,
   `HttpOnly`, `SameSite=Strict` bridge cookie, and redirects to Open WebUI.
   Every HTTP request and WebSocket handshake revalidates the bridge session,
   session family, user state, and `inference.use`; active WebSockets are
   periodically revalidated and closed on revocation. Client-supplied trusted
   identity headers are stripped before the gateway injects them.
5. **Open WebUI lockdown.** Configure the patched image with only the managed
   inference-manager OpenAI `/v1` provider. Disable local login/signup,
   first-user admin, password recovery, API keys, provider/model management,
   direct Ollama, uploads/RAG/tools/connectors/web search/audio, and external
   identity/avatar calls. Use opaque appliance user IDs only.
6. **Deployment/routing.** Extend the inference chart with gateway health
   checks, secrets, restricted NetworkPolicies, and an appliance-managed HTTPS
   port route to the gateway. The core appliance readiness must not depend on
   WebUI. The bridge cookie must be ignored/stripped by non-WebUI routes.
7. **Installer/release wiring.** Make the image pair required together in new
   LLM packs, preload them, pass digest values to Helm, and keep old bundles
   inference-only. Update release contracts, offline docs, tests, and pack
   resolver assertions.
8. **Appliance UI.** Add an enabled-state-aware “Open AI Workspace” action to
   AI Services. It calls the launch API and form-POSTs the raw grant to the
   gateway; it must not recreate chat UI, place a grant in a URL, or persist it
   in JavaScript storage. Explain unavailable states (no LLM pack, no loaded
   model, WebUI not ready, or missing permission).
9. **End-to-end verification.** Cover successful launch; expired/replayed
   grant; API-token rejection; permission/user/session revocation; restart;
   SSE; WebSocket close after revocation; direct Open WebUI denial; no public
   egress; backup/restore; and foundation-pack absence.

## Ordered work

1. Verify `source.lock`, fetch the exact source only in online build mode, and
   apply the numbered appliance patches with `USE_SLIM=true`.
2. Build the non-root/read-only/egress-denied candidate and run
   `tests/gate-smoke.sh`, extended for provider lock, SSE, and WebSockets.
3. Add `deps/open-webui` in `appliance-release`; offline builds consume only
   its LAN-seeded source/image input.
4. Export `registry.local/open-webui:bundled` and its platform digest from
   `appliance-code`; add both release-input and `zonctl` consumers.
5. Deploy the image with the WebUI gateway, separate RWO PVC, and restricted
   ingress/egress policy only when an LLM pack is installed.
6. Implement the one-time appliance launch grant and opaque bridge session
   before exposing the UI route.

The gateway and WebUI deployment are not allowed to enter the release until
the source/image compatibility gate has passed on the exact digest.
