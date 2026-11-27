# Log Collection Pattern

Runtime logs live under the appliance-owned root `/data/zon/logs`. Each
long-running service has a stable directory containing `stdout.log`,
`stderr.log`, and, where supported, `application.log`.

Current service directories include:

```text
/data/zon/logs/api-server/
/data/zon/logs/ui/
/data/zon/logs/action-runtime/
/data/zon/logs/artifactserver/
/data/zon/logs/dns/
```

The path is a product constant, not a Helm value or profile-specific setting.
Operators can inspect the same services with `kubectl logs`; the appliance
directories provide a stable host-facing support and diagnostics interface.

Application containers remain non-root and read-only. Service-owned log files
are writable by the service identity and host-user readable/traversable.
