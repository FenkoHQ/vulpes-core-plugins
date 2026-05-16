# upstream-openai

OpenAI-compatible upstream provider.

Example config:

```yaml
config:
  base_url: https://api.openai.com/v1
  api_key: ${secret:OPENAI_API_KEY}
```

The MVP invokes `/chat/completions` with normalized OpenAI-compatible requests and returns normalized chunks/usage to the gateway core.

Routers may override the configured `base_url` per request by returning selected route property `base_url`. This is how `router-consul` can discover multiple LiteLLM/OpenAI-compatible instances and route them through one upstream provider instance.

If route properties include pricing (`input_cost_per_1k_tokens` / `output_cost_per_1k_tokens`, or compatible aliases), the plugin computes `usage.cost_usd` from returned token usage. `router-litellm` propagates these properties from LiteLLM-style `model_list` entries.
