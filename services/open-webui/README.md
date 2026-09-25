# Open WebUI appliance adaptation (compatibility spike)

This is **not yet a release image**. Do not add it to the appliance chart or
air-gap bundle until the compatibility gate in
`docs/open-webui-integration-plan.md` passes on the exact built image.

The implementation order and package boundary are recorded in
[`IMPLEMENTATION.md`](IMPLEMENTATION.md). Open WebUI is an optional `std-llm`
or `acc-llm` artifact only; `foundation` must remain free of its source,
image, chart, gateway, and storage resources.

The reviewed source is Open WebUI `v0.11.4`, commit
`8bd8b4fac5e059578ac0c74b3c18d11139f88b7d`. Apply both patches in
`patches/` in numeric order to that exact commit. The auth patch separates
trusted-header sign-in from local password authentication, denies local signup
when password authentication is disabled, and prevents trusted-header accounts
from receiving first-user admin rights. The UI patch stops sending the internal
account key as a chat prompt variable and removes Gravatar actions that would
transmit it to an external service. The build patch raises the frontend-only
Node heap ceiling after the upstream default exhausted its heap during the
`v0.11.4` production bundle build on the Linux dev-container host. The fourth
patch omits production source maps; otherwise the nested builder exhausted its
file-descriptor limit while writing them. The compatibility build also raises
its build-container `nofile` limit.
The fifth patch removes the separate editable Profile page in trusted-header
mode and rejects profile mutations in the API; the appliance owns display
identity. The sixth patch removes the upstream production build's unconditional
Pyodide download. The appliance build must use `USE_SLIM=true`; that is the
upstream mode which omits local embedding, speech, and document-processing
model downloads. The gate rejects a non-slim image.

The former upstream-default compatibility build is rejected: it downloaded
Pyodide assets and Hugging Face models while constructing the image. The
appliance compatibility image must be built with `USE_SLIM=true` and this
patch set; no prior image ID is a release candidate. `tests/gate-smoke.sh`
requires the slim image and checks egress-denied startup under UID/GID 10011,
read-only root, dropped capabilities, separate writable data/cache volumes,
and `STATIC_DIR=/app/backend/data/static`. The test denies local signup,
requires the trusted header for sign-in, verifies the first user remains
ordinary, denies that user native admin access, and rejects edits to
appliance-owned profile fields.

The patch **does not** make the release ready. Before building the image,
finish the identity/UI review (including any synthetic address still shown to
users), lock down native provider and account
management, and design the appliance session bridge. Build from pinned,
verified inputs on a Linux host. Test the resulting digest under the intended
non-root UID/GID, read-only root filesystem, explicit writable mounts, and
public-egress-denied network. Then exercise trusted login, forbidden local
login/signup, user role, chat streaming, WebSockets, engine changes, and
restart/restore against that same digest.

Upstream source and license: https://github.com/open-webui/open-webui/tree/v0.11.4
