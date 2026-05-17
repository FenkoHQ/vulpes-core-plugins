# observer-prometheus

Prometheus exporter observer for Vulpes Core gateway events.

The plugin receives async gateway lifecycle events and exposes Prometheus text metrics over HTTP.

Default endpoint:

```text
http://127.0.0.1:9090/metrics
```

Example config:

```yaml
config:
  listen: 127.0.0.1:9090
  path: /metrics
  namespace: vulpes_gateway
  enable_tenant_labels: false
  model_catalog:
    - model: gpt-5.5
      provider: codex
      upstream_model: gpt-5.5
      base_url: https://chatgpt.com/backend-api/codex
      endpoint: http://vulpes-gateway.service.den.internal:18088/v1/chat/completions
```

Metrics:

- `vulpes_gateway_events_total{event_type=...}`
- `vulpes_gateway_requests_total{status=completed|failed}`
- `vulpes_gateway_model_info{model=...,provider=...,upstream_model=...,base_url=...,endpoint=...}`
- `vulpes_gateway_usage_input_tokens_total{provider=...,model=...}`
- `vulpes_gateway_usage_output_tokens_total{provider=...,model=...}`
- `vulpes_gateway_usage_total_tokens_total{provider=...,model=...}`
- `vulpes_gateway_usage_cost_usd_total{provider=...,model=...}`

Tenant labels are disabled by default to avoid high-cardinality metrics. Enable only when you know the tenant cardinality is safe for your Prometheus setup.

`model_catalog` is optional static metadata for dashboards. It should contain only stable, low-cardinality routable aliases, not every discovered upstream raw model.
