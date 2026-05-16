# upstream-codex

Codex-style upstream provider using the OpenAI Responses API (`/v1/responses`).

This plugin is separate from `upstream-openai` because Codex/code models are typically driven through the Responses API rather than Chat Completions. It still implements Vulpes' typed `upstream_provider` capability and returns normalized chat chunks plus token usage.

Example config:

```yaml
- name: codex
  source:
    type: filesystem
    path: /path/to/upstream-codex
  capabilities: [upstream_provider]
  fail_mode: closed
  config:
    base_url: https://api.openai.com/v1
    api_key: ${secret:CODEX_API_KEY}
    reasoning_effort: medium
    timeout_seconds: 180
```

Example model alias:

```yaml
models:
  aliases:
    codex:
      candidates:
        - provider: codex
          model: gpt-5-codex
          weight: 100
```

Notes:

- Public model names remain the Vulpes aliases (`codex`, `alias1`, etc.).
- Metrics report `model=<public alias>` and `upstream_model=<provider model>` when paired with a recent Vulpes core/Prometheus observer.
- Cost is computed from route properties when provided (`input_cost_per_1k_tokens`, `output_cost_per_1k_tokens`).
- Streaming is normalized from Responses SSE `response.output_text.delta`-style events.
