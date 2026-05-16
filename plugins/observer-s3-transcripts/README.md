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
      gzip: true
      database_url: ${secret:TRANSCRIPTS_DATABASE_URL} # optional index
      database_table: vulpes_transcripts
```

Objects are compact JSON and gzip-compressed by default. Set `gzip: false` to store plain JSON, or `pretty: true` for indented JSON before compression.

Objects are written as:

```text
<prefix>/YYYY/MM/DD/HH/<request_id>.<event_type>.json.gz
```

By default it stores `request.completed` and `request.failed`. Set `store_all_events: true` to store every observer event.

If `database_url` is configured, the plugin also writes an index row to Postgres containing tenant/model/usage/object-key/byte-size metadata and JSONB properties/errors. The full transcript remains in R2/S3.
