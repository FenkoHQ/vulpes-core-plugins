# Vulpes Core Plugins

MVP plugin set for `github.com/FenkoHQ/vulpes-core`.

Included plugins:

- `authn-static-api-key`: static API-key authenticator for local/dev/self-hosted deployments.
- `router-weighted`: weighted/shuffle/ordered router.
- `upstream-openai`: OpenAI-compatible upstream provider.
- `observer-stdout`: simple JSON event sink for development.

## Build

```bash
make test
make lint
make build
```

Binaries are written to `bin/`.

## Example gateway config

See `examples/gateway.yaml`.

The current MVP plugins speak the core's local Unix-socket RPC protocol and expose metadata, config schema, configure, health, and strict capability methods.
