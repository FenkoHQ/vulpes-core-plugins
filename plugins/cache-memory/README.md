# cache-memory

In-process TTL cache provider for Vulpes Core.

This plugin keeps cache state inside the plugin process, not in the stateless core. It is useful for dev, canaries, and single-instance deployments. For HA production, prefer a future shared backend cache plugin such as Redis.

Config:

```yaml
config:
  default_ttl_ms: 60000
  max_entries: 10000
  namespace: prod
  include_model: true
```

Notes:

- Streaming responses are not cached by the core.
- The core computes the cache key from request, tenant, and request properties.
- Entries are evicted by expiry and least-recently-used order when `max_entries` is exceeded.
