# Auth Service

`auth-service` is the application-auth capability boundary. It is deployed in
the dedicated `ace-app-core` namespace, before `ace-apps` workloads.

The service owns mobile OTP challenge issuance, verification, and browser
session creation. It uses the shared PostgreSQL auth tables and publishes the
provider-neutral `event.identity.otp.requested` event when event delivery is
enabled. In local/test delivery mode it keeps the code in a bounded process
inbox for development-only use.

`application-identity` owns onboarding and domain records. Its public auth
routes are a compatibility proxy to the URL configured by
`APPLICATION_IDENTITY_AUTH_SERVICE_URL`; that URL can be the in-cluster
`auth-service.ace-app-core.svc.cluster.local` endpoint or a deployment-time
external Auth Service endpoint.

For local/test delivery, retrieve a pending code only from the private socket
inside the Auth Service pod:

```sh
SOCKET=$(kubectl -n ace-app-core exec deploy/auth-service -- sh -c 'find /var/run/zon-auth -name inbox.sock -print -quit')
kubectl -n ace-app-core exec deploy/auth-service -- /auth-service take-code --socket "$SOCKET" --phone +919876543210
```
