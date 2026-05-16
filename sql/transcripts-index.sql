CREATE TABLE IF NOT EXISTS vulpes_transcripts (
  request_id text NOT NULL,
  event_type text NOT NULL,
  tenant_id text NULL,
  timestamp_unix_nano bigint NOT NULL,
  stored_at timestamptz NOT NULL DEFAULT now(),
  provider text NULL,
  model text NULL,
  route_provider text NULL,
  route_model text NULL,
  input_tokens bigint NOT NULL DEFAULT 0,
  output_tokens bigint NOT NULL DEFAULT 0,
  total_tokens bigint NOT NULL DEFAULT 0,
  cost_usd double precision NOT NULL DEFAULT 0,
  bucket text NOT NULL,
  object_key text NOT NULL,
  uncompressed_bytes integer NOT NULL DEFAULT 0,
  stored_bytes integer NOT NULL DEFAULT 0,
  gzip boolean NOT NULL DEFAULT true,
  properties jsonb NOT NULL DEFAULT '{}'::jsonb,
  error jsonb NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (request_id, event_type)
);

CREATE INDEX IF NOT EXISTS vulpes_transcripts_tenant_time_idx ON vulpes_transcripts (tenant_id, timestamp_unix_nano DESC);
CREATE INDEX IF NOT EXISTS vulpes_transcripts_model_time_idx ON vulpes_transcripts (model, timestamp_unix_nano DESC);
CREATE INDEX IF NOT EXISTS vulpes_transcripts_event_time_idx ON vulpes_transcripts (event_type, timestamp_unix_nano DESC);
