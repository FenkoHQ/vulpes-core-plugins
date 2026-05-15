# observer-s3-transcripts

Stores request transcripts and failure records in S3-compatible object storage.

This is the Vulpes equivalent of the S3/request-log integrations commonly used around LiteLLM/Helicone-style gateways.

To include prompt/response payloads, enable the core capture switch:

```yaml
observability:
  capture_payloads: true
```

Example config:

```yaml
plugins:
  - name: s3-transcripts
    source:
      type: filesystem
      path: ./bin/observer-s3-transcripts
    capabilities: [observer]
    fail_mode: open
    config:
      endpoint: https://s3.example.com
      region: us-east-1
      bucket: llm-transcripts
      prefix: vulpes/prod
      access_key_id: ${secret:S3_ACCESS_KEY_ID}
      secret_access_key: ${secret:S3_SECRET_ACCESS_KEY}
      force_path_style: true
```

Objects are written as JSON:

```text
<prefix>/YYYY/MM/DD/HH/<request_id>.<event_type>.json
```

By default it stores `request.completed` and `request.failed`. Set `store_all_events: true` to store every observer event.
