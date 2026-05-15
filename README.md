# Vulpes Core Plugins

MVP plugin set for `github.com/FenkoHQ/vulpes-core`.

Included plugins:

- `authn-static-api-key`: static API-key authenticator for local/dev/self-hosted deployments.
- `router-weighted`: simple weighted/shuffle/ordered router.
- `router-litellm`: LiteLLM-style router with load balancing, fallbacks, rpm/tpm limits, cooldowns, and feedback metrics.
- `upstream-openai`: OpenAI-compatible upstream provider.
- `observer-stdout`: simple JSON event sink for development.
- `observer-prometheus`: Prometheus `/metrics` exporter for gateway events and usage.
- `observer-otel`: OTLP/HTTP traces and metrics exporter.
- `observer-s3-transcripts`: transcript/request-log writer for S3-compatible storage.

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
