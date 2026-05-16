# Vulpes Core Plugins

MVP plugin set for `github.com/FenkoHQ/vulpes-core`.

Included plugins:

- `authn-static-api-key`: static API-key authenticator for local/dev/self-hosted deployments.
- `authn-postgres-api-key`: database-backed API key authenticator.
- `cache-memory`: in-process TTL cache provider.
- `ratelimit-memory`: in-process fixed-window request/token rate limiter.
- `router-weighted`: simple weighted/shuffle/ordered router.
- `router-litellm`: LiteLLM-style router with load balancing, fallbacks, rpm/tpm limits, cooldowns, and feedback metrics.
- `router-consul`: discovers healthy LiteLLM/OpenAI-compatible instances from Consul.
- `prompt-context-injector`: injects or replaces prompt context by key, model, tenant, or subject.
- `prompt-template-registry`: static versioned prompt registry with template rendering.
- `upstream-openai`: OpenAI-compatible upstream provider.
- `upstream-codex`: Codex/code-model upstream provider using the OpenAI Responses API.
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
