# observer-otel

OpenTelemetry observer for Vulpes Core.

Exports gateway lifecycle events as OTLP traces and aggregate counters as OTLP metrics using OTLP/HTTP.

Example config:

```yaml
plugins:
  - name: otel
    source:
      type: filesystem
      path: ./bin/observer-otel
    capabilities: [observer]
    fail_mode: open
    config:
      endpoint: localhost:4318
      service_name: vulpes-gateway
      insecure: true
      resource_attributes:
        deployment.environment: prod
```

For collectors that require one exact OTLP/HTTP protobuf URL, set the full URL as `endpoint`, for example:

```yaml
endpoint: http://siem.example.com:9428/insert/opentelemetry
```

The plugin does not attach raw prompt/response payloads to spans. If the core is configured with `observability.capture_payloads: true`, payloads are intentionally ignored by this plugin to avoid accidental telemetry leakage. Use `observer-s3-transcripts` for transcript storage.
