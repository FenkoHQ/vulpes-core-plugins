# observer-stdout

Development observer that writes gateway lifecycle events as JSON lines to stdout.

Example config:

```yaml
config:
  pretty: false
  include_payloads: false
```

When `include_payloads` is `false` (or `omit_payloads` is `true`), `request_json` and `response_json` are removed from log events and replaced with omission markers. Lookup fields such as `request_id`, `tenant_id`, `event_type`, timestamp, model, and route metadata remain, so operators can find the corresponding transcript object through the `vulpes_transcripts` index.
