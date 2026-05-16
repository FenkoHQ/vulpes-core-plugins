CREATE TABLE IF NOT EXISTS vulpes_api_keys (
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

CREATE INDEX IF NOT EXISTS vulpes_api_keys_key_hash_active_idx ON vulpes_api_keys (key_hash) WHERE active = true;
CREATE INDEX IF NOT EXISTS vulpes_api_keys_tenant_idx ON vulpes_api_keys (tenant_id);
