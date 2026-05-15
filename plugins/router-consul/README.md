# router-consul

Consul service discovery router.

Discovers healthy LiteLLM/OpenAI-compatible service instances from Consul and returns gateway routes.

Expected Consul tags or metadata:

- `model:<logical-model>`
- `provider_model:<upstream-model>` optional
- `base_url:<http://host:port/v1>` optional

If `base_url` is absent, it builds `<scheme>://<service-address>:<port>/v1`.

Pair with `upstream-openai`; route properties override its base URL per request.

```yaml
plugins:
  - name: consul-router
    capabilities: [router]
    config:
      address: http://127.0.0.1:8500
      service: litellm
      provider_instance: litellm

  - name: litellm
    capabilities: [upstream_provider]
    config:
      base_url: http://placeholder/v1
      api_key: ${secret:LITELLM_API_KEY}
```
