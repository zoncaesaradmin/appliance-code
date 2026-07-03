# E2E Tests

This directory is reserved for external end-to-end test harnesses that
exercise the real control-plane server through its public SDK and protocol
surfaces.

The detailed plan is in [../docs/e2e-testing-plan.md](../docs/e2e-testing-plan.md).

Target shape:

```text
e2etests/
  go.mod
  internal/
  local/
    integration-test/
  appliance/
    integration-test/
```

Rules for this area:

- tests here must behave like external clients
- they must not import backend `internal/...` packages
- REST API flows should go through `server/sdk/golang/applianceclient`
- local E2E must run without K3s, packaging, or containers
- installed-appliance validation should reuse these clients from the release
  repo where possible
