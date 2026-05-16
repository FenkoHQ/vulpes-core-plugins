# router-litellm

LiteLLM-style router for Vulpes Core.

Supported MVP behavior:

- model groups via `model_list`
- fallback model groups via `fallbacks`
- context/content-policy fallback config parsing
- routing strategies:
  - `simple-shuffle`
  - `weighted`
  - `least-busy`
  - `usage-based-routing`
  - `latency-based-routing`
  - `fallback` / `ordered`
- per-deployment `rpm` / `tpm` filtering
- per-deployment cost metadata (`input_cost_per_token`, `output_cost_per_token`, or per-1k variants) propagated to upstream usage accounting
- cooldown after repeated upstream failures
- observer feedback loop for usage, latency, failures, and in-flight counts

Use the same plugin instance as both router and observer to enable feedback:

```yaml
pipeline:
  router: litellm-router
  observers: [litellm-router]
```

Example config:

```yaml
config:
  routing_strategy: least-busy
  allowed_fails: 3
  failure_window_seconds: 60
  cooldown_time_seconds: 30
  model_list:
    - model_name: gpt-4o-mini
      provider_instance: openai-a
      model: gpt-4o-mini
      weight: 100
      rpm: 1000
      tpm: 100000
    - model_name: gpt-4o-mini
      provider_instance: openai-b
      model: gpt-4o-mini
      weight: 100
  fallbacks:
    gpt-4: [gpt-4o-mini]
```
