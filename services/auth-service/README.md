# Auth Service

`auth-service` is the application-auth capability boundary. It is deployed in
the dedicated `ace-app-core` namespace, before `ace-apps` workloads.

The first runtime slice exposes health and the application capability
contract. Existing mobile OTP issuance and session creation remain in
`application-identity` until the provider-backed auth implementation is moved
behind this boundary.
