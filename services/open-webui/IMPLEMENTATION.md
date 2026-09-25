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
