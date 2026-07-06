# Curl Test

This directory contains a live-server curl-based reference flow for the
control-plane HTTP API.

The goal is different from the Go SDK end-to-end lane:

- this flow proves the raw HTTP surface is locally runnable
- it gives copy-pasteable endpoint examples
- it catches routing, auth, and envelope regressions without depending on
  the SDK client

Run it with:

```sh
make -C services/controlplane test-curl
```
