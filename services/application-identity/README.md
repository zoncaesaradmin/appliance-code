# Application identity

This service owns the Zonited application onboarding and identity-domain
contract. Mobile OTP challenge issuance, verification, and browser session
creation are owned by the `auth-service` capability. It is separate from the
appliance operator authentication API.

The public API is:

- `GET /api/app/v1/auth/capabilities` for the Auth Service's enabled authentication
  methods and delivery channels
- `POST /api/app/v1/auth/otp/challenges` with `{"phone":"+..."}`
- `POST /api/app/v1/auth/otp/verify` with `{"challenge_id":"...","code":"123456"}`
- `GET /api/app/v1/auth/session`
- `POST /api/app/v1/auth/logout`
- `GET /api/app/v1/onboarding/memberships` for the signed-in user's active
  Zonity memberships
- Nearby-zone responses include each active zone's latitude, longitude, and
  calculated distance so onboarding clients can show a map confirmation.
- `POST /api/app/v1/onboarding/nearby` with a signed-in session or a
  short-lived channel discovery token and `{\"latitude\":…,\"longitude\":…}`
- `POST /api/app/v1/onboarding/join` with an authenticated session and a
  selected public zone ID
- `POST /api/app/v1/onboarding/zone-requests` with an authenticated session
  and a requested Zonity name

The public auth paths remain available through this service as a routing
boundary, but are always forwarded to the deployment-configured Auth Service
endpoint. The identity service also asks that endpoint to resolve an
authenticated principal for onboarding through the internal
`/internal/v1/auth/principal` endpoint; it does not implement OTP, session,
or browser-authentication storage itself. The Auth Service owns the local
development inbox and publishes the encrypted `event.identity.otp.requested`
event in integrated deployments.

Auth Service initializes the shared authentication tables. This service
initializes only onboarding/domain tables. Request rate-limit and internal
boundary keys are supplied by the installer through target-generated
Kubernetes Secrets; the plaintext keys are never logged or included in a bundle.

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
the channel discovery link alone is read-only. Join requests are accepted only
within each zone's join radius (1 km by default), and are auto-approved only
when that zone has no active Zonity admin; otherwise they remain pending for
Zonity admin review. There is no end-user zone creation API; creation is reserved
for authorized tenant/platform staff. The Auth Service's web OTP flow remains
bound to the `customer-web` channel and keeps the existing public API contract.
The channel integration can use the same contract when its identity resolver
is configured; it passes only an opaque channel subject and normalized identity
attribute, never provider identities or caller-controlled user headers to
application APIs. A resolved channel interaction receives a short-lived
`/app/discover` link; the browser remains the authoritative location submitter.

For a local/test smoke test, an operator with access to the pod can retrieve a
pending code without exposing it publicly:

```sh
SOCKET=$(kubectl -n ace-app-core exec deploy/auth-service -- sh -c 'find /var/run/zon-auth -name inbox.sock -print -quit')
kubectl -n ace-app-core exec deploy/auth-service -- /auth-service take-code --socket "$SOCKET" --phone +919876543210
```
