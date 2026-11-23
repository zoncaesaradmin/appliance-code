# Application identity

This service owns the Zonited application mobile sign-in contract. It is
separate from the appliance operator authentication API.

The public API is:

- `POST /api/shared/v1/auth/otp/challenges` with `{"phone":"+..."}`
- `POST /api/shared/v1/auth/otp/verify` with `{"challenge_id":"...","code":"123456"}`
- `GET /api/shared/v1/auth/session`
- `POST /api/shared/v1/auth/logout`

The current delivery adapter is only the CAPP-compatible `local`/`test`
development inbox. Codes are held in memory and can be retrieved only through
the owner-only Unix socket created under `APPLICATION_IDENTITY_OTP_SOCKET_PARENT`.
The socket is never exposed through a Kubernetes Service or ingress. The
service refuses any other environment until a production SMS/provider adapter,
abuse controls, and delivery observability are reviewed and implemented.

The service creates the identity tables if they do not exist. OTP and request
rate-limit keys are supplied by the installer through a target-generated
Kubernetes Secret; the plaintext keys are never logged or included in a bundle.

For a local/test smoke test, an operator with access to the pod can retrieve a
pending code without exposing it publicly:

```sh
SOCKET=$(kubectl -n ace-apps exec deploy/identity -- sh -c 'find /var/run/zonited-identity -name inbox.sock -print -quit')
kubectl -n ace-apps exec deploy/identity -- /application-identity take-code --socket "$SOCKET" --phone +919876543210
```
