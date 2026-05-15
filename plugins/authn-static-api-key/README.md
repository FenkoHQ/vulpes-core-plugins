# authn-static-api-key

Static API key authenticator.

Accepted credentials:

- `Authorization: Bearer <key>`
- `X-API-Key: <key>`

Example config:

```yaml
config:
  keys:
    - id: local-dev
      value: ${secret:GATEWAY_API_KEY}
      tenant: dev
      subject: local-dev
```
