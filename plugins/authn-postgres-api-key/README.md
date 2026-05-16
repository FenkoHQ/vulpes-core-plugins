# authn-postgres-api-key

Database-backed API key authenticator.

Keys are stored by SHA-256 hash, not plaintext.

## Schema

The plugin can create this table automatically with `auto_create: true`:

```sql
CREATE TABLE vulpes_api_keys (
  key_id text PRIMARY KEY,
  key_hash text NOT NULL UNIQUE,
  subject text NOT NULL,
  tenant_id text NOT NULL,
  groups text[] NOT NULL DEFAULT '{}',
  active boolean NOT NULL DEFAULT true,
  expires_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
```

Generate a hash:

```bash
printf %s 'your-api-key' | sha256sum | awk '{print "sha256:"$1}'
```

Insert a key:

```sql
INSERT INTO vulpes_api_keys (key_id, key_hash, subject, tenant_id, groups)
VALUES ('prod-ali', 'sha256:...', 'ali', 'prod', ARRAY['admin']);
```

Config:

```yaml
config:
  database_url: ${secret:AUTH_DATABASE_URL}
  table: vulpes_api_keys
  auto_create: true
```
