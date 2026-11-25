# Service lifecycle contract

Appliance services use the same dependency lifecycle contract. A temporary
failure of a runtime dependency must not become a container restart.

## Probe semantics

- `GET /health/live` reports only that the process event loop is alive. It must
  not query PostgreSQL, the message broker, blob storage, or another service.
- `GET /health/ready` reports whether the service can accept its owned traffic.
  It checks the service's required dependencies and schema state. Kubernetes
  only routes Service traffic to a ready pod.
- `GET /health/startup`, when a chart uses it, reports that the process has
  completed its own one-time initialization. A dependency outage belongs in
  readiness, not liveness.

During dependency recovery, the expected state is `Running` with `0/1 Ready`,
not a growing restart count. API requests other than health checks must return
a safe unavailable response until readiness is restored.

## Retry policy

Direct PostgreSQL consumers use `sdk/golang/servicehealth`:

- attempt timeout: 5 seconds;
- retry delays: 1, 2, 4, 8, 16, then 30 seconds;
- after reaching 30 seconds, continue checking every 30 seconds;
- retry until the process context is cancelled.

Static configuration errors, such as a missing required secret or invalid
origin, still fail fast during configuration loading. Runtime connection and
schema availability errors remain live-but-unready and are logged at the capped
retry interval.

## Current consumers

| Service | PostgreSQL role | Lifecycle behavior |
| --- | --- | --- |
| `ordering` | Own schema and order data | Health listener starts first; schema initialization retries indefinitely. |
| `identity` | Own OTP, session, tenant, channel, and onboarding data | Health listener starts first; schema initialization retries indefinitely. |
| `controlplane` | Shared platform schema and migrations | Bootstrap health listener stays live while storage opens and migrates; the full API starts only after storage is ready. |

Services without a direct PostgreSQL dependency do not wait for PostgreSQL.
They retain their own dependency-specific readiness checks.
