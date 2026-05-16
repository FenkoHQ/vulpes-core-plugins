# ratelimit-memory

Fixed-window in-process rate limiter for Vulpes Core.

This keeps counters inside the plugin process, not the core. It is useful for dev, canaries, and single-instance deployments. For multi-gateway production, use it only when per-instance limits are acceptable, or replace it later with a shared Postgres/Redis-backed limiter.

Config:

```yaml
config:
  requests_per_minute: 120
  tokens_per_minute: 200000
  key_by: tenant_model # tenant, subject, tenant_model, subject_model
  rules:
    - name: admin-gpt4o
      tenant: prod
      group: admin
      model: gpt-4o
      requests_per_minute: 600
      tokens_per_minute: 1000000
```

The plugin reserves request and estimated-token capacity during `Check`. `Commit` is intentionally a no-op to avoid double-counting.
