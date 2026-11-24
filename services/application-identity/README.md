# Application identity

This service owns the Zonited application mobile sign-in contract. It is
separate from the appliance operator authentication API.

The public API is:

- `POST /api/shared/v1/auth/otp/challenges` with `{"phone":"+..."}`
- `POST /api/shared/v1/auth/otp/verify` with `{"challenge_id":"...","code":"123456"}`
- `GET /api/shared/v1/auth/session`
- `POST /api/shared/v1/auth/logout`
- `POST /api/shared/v1/onboarding/nearby` with a signed-in session or a
  short-lived channel discovery token and `{\"latitude\":…,\"longitude\":…}`
- `POST /api/shared/v1/onboarding/join` with an authenticated session and a
  selected public zone ID
- `POST /api/shared/v1/onboarding/zone-requests` with an authenticated session
  and a requested Zonity name

The current delivery adapter is only the CAPP-compatible `local`/`test`
development inbox. Codes are held in memory and can be retrieved only through
the owner-only Unix socket created under `APPLICATION_IDENTITY_OTP_SOCKET_PARENT`.
The socket is never exposed through a Kubernetes Service or ingress. The
service refuses any other environment until a production SMS/provider adapter,
abuse controls, and delivery observability are reviewed and implemented.

The service creates the identity tables if they do not exist. OTP and request
rate-limit keys are supplied by the installer through a target-generated
Kubernetes Secret; the plaintext keys are never logged or included in a bundle.

The identity schema now has the first generic application-principal slice:
the explicit `zonited-solutions` tenant (display name `Zonited Solutions`), server-side channel policies, keyed channel
subject mappings, and channel-bound short-lived sessions. A trusted adapter
may call `POST /internal/v1/application-principal/resolve` with
`{"tenant_id":"zonited-solutions","channel_id":"...","subject_id":"...","phone":"+..."}`
and the target-generated `X-Application-Identity-Key` header. The response
contains an opaque short-lived session token and a provider-neutral principal.
This endpoint is internal-only; provider verification and mapping stay in the
adapter, and the response contains no phone number or provider identifier.

The onboarding slice creates an identified user, returns up to five active
public zones within a server-capped 10 km radius, and records a pending join
request. Joining and requesting a new Zonity require the application session;
the channel discovery link alone is read-only. It does not yet grant zone
membership, vendor approval, role grants, or application permissions. There is
no end-user zone creation API; creation is reserved for authorized
tenant/platform staff. The web OTP adapter remains
bound to the `customer-web` channel and keeps the existing public API contract.
The channel integration can use the same contract when its identity resolver
is configured; it passes only an opaque channel subject and normalized identity
attribute, never provider identities or caller-controlled user headers to
application APIs. A resolved channel interaction receives a short-lived
`/app/discover` link; the browser remains the authoritative location submitter.

For a local/test smoke test, an operator with access to the pod can retrieve a
pending code without exposing it publicly:

```sh
SOCKET=$(kubectl -n ace-apps exec deploy/identity -- sh -c 'find /var/run/zonited-identity -name inbox.sock -print -quit')
kubectl -n ace-apps exec deploy/identity -- /application-identity take-code --socket "$SOCKET" --phone +919876543210
```
