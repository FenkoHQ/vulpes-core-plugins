# upstream-openai

OpenAI-compatible upstream provider.

Example config:

```yaml
config:
  base_url: https://api.openai.com/v1
  api_key: ${secret:OPENAI_API_KEY}
```

The MVP invokes `/chat/completions` with normalized OpenAI-compatible requests and returns normalized chunks/usage to the gateway core.
