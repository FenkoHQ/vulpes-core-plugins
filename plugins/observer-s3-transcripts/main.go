package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
)

type plugin struct {
	mu sync.Mutex

	bucket         string
	prefix         string
	endpoint       string
	region         string
	accessKey      string
	secretKey      string
	sessionToken   string
	forcePathStyle bool
	storeEvents    bool
	gzip           bool
	pretty         bool
	databaseURL       string
	databaseTable     string
	databaseAuto      bool
	bypassTenantIDs  map[string]struct{}
	client            *s3.Client
	pg                *pgxpool.Pool
}

type transcriptObject struct {
	Version    string            `json:"version"`
	StoredAt   string            `json:"stored_at"`
	RequestID  string            `json:"request_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	EventType  string            `json:"event_type"`
	Timestamp  int64             `json:"timestamp_unix_nano"`
	Properties map[string]string `json:"properties,omitempty"`
	Usage      sdk.Usage         `json:"usage,omitempty"`
	Error      map[string]string `json:"error,omitempty"`
	Request    json.RawMessage   `json:"request,omitempty"`
	Response   json.RawMessage   `json:"response,omitempty"`
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.bucket = stringValue(cfg["bucket"])
	p.prefix = strings.Trim(stringValue(cfg["prefix"]), "/")
	p.endpoint = stringValue(cfg["endpoint"])
	p.region = firstString(cfg["region"], "us-east-1")
	p.accessKey = firstString(cfg["access_key_id"], cfg["access_key"], secrets["R2_ACCESS_KEY_ID"], secrets["S3_ACCESS_KEY_ID"], secrets["AWS_ACCESS_KEY_ID"])
	p.secretKey = firstString(cfg["secret_access_key"], cfg["secret_key"], secrets["R2_SECRET_ACCESS_KEY"], secrets["S3_SECRET_ACCESS_KEY"], secrets["AWS_SECRET_ACCESS_KEY"])
	p.sessionToken = firstString(cfg["session_token"], secrets["AWS_SESSION_TOKEN"])
	p.forcePathStyle = true
	if v, ok := cfg["force_path_style"].(bool); ok {
		p.forcePathStyle = v
	}
	p.storeEvents = false
	if v, ok := cfg["store_all_events"].(bool); ok {
		p.storeEvents = v
	}
	p.gzip = true
	if v, ok := cfg["gzip"].(bool); ok {
		p.gzip = v
	}
	p.pretty = false
	if v, ok := cfg["pretty"].(bool); ok {
		p.pretty = v
	}
	p.databaseURL = firstString(cfg["database_url"], cfg["postgres_url"], secrets["TRANSCRIPTS_DATABASE_URL"], secrets["DATABASE_URL"])
	p.databaseTable = firstString(cfg["database_table"], "vulpes_transcripts")
	p.bypassTenantIDs = parseTenantSet(cfg["bypass_tenant_ids"], cfg["stealth_tenant_ids"])
	p.databaseAuto = true
	if v, ok := cfg["database_auto_create"].(bool); ok {
		p.databaseAuto = v
	}
	if p.bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if p.accessKey == "" || p.secretKey == "" {
		return fmt.Errorf("access_key_id and secret_access_key are required")
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(p.region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(p.accessKey, p.secretKey, p.sessionToken)),
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
	}
	if p.endpoint != "" {
		opts = append(opts, config.WithBaseEndpoint(p.endpoint))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return err
	}
	p.client = s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = p.forcePathStyle })
	if p.databaseURL != "" {
		pool, err := pgxpool.New(ctx, p.databaseURL)
		if err != nil {
			return err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return err
		}
		p.pg = pool
		if p.databaseAuto {
			return p.createTranscriptSchema(ctx)
		}
	}
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	p.mu.Lock()
	client := p.client
	bucket := p.bucket
	prefix := p.prefix
	storeAll := p.storeEvents
	gzipEnabled := p.gzip
	pretty := p.pretty
	pg := p.pg
	databaseTable := p.databaseTable
	bypassTenantIDs := copyTenantSet(p.bypassTenantIDs)
	p.mu.Unlock()
	if client == nil {
		return nil
	}
	for _, ev := range events {
		if shouldBypassTenant(ev.TenantID, bypassTenantIDs) {
			continue
		}
		if !storeAll && ev.EventType != "request.completed" && ev.EventType != "request.failed" {
			continue
		}
		obj := buildTranscript(ev)
		body, err := marshalTranscript(obj, pretty)
		if err != nil {
			return err
		}
		key := transcriptKey(prefix, ev, gzipEnabled)
		storedBody := body
		input := &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(storedBody),
			ContentType: aws.String("application/json"),
		}
		if gzipEnabled {
			compressed, err := gzipBytes(body)
			if err != nil {
				return err
			}
			storedBody = compressed
			input.Body = bytes.NewReader(storedBody)
			input.ContentEncoding = aws.String("gzip")
		}
		_, err = client.PutObject(ctx, input)
		if err != nil {
			return fmt.Errorf("put transcript %s/%s: %w", bucket, key, err)
		}
		if pg != nil {
			if err := indexTranscript(ctx, pg, databaseTable, ev, obj, bucket, key, len(body), len(storedBody), gzipEnabled); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *plugin) createTranscriptSchema(ctx context.Context) error {
	tableIdent := pgIdent(p.databaseTable)
	tenantIndexIdent := pgIdent(p.databaseTable + "_tenant_time_idx")
	modelIndexIdent := pgIdent(p.databaseTable + "_model_time_idx")
	eventIndexIdent := pgIdent(p.databaseTable + "_event_time_idx")
	q := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
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
CREATE INDEX IF NOT EXISTS %s ON %s (tenant_id, timestamp_unix_nano DESC);
CREATE INDEX IF NOT EXISTS %s ON %s (model, timestamp_unix_nano DESC);
CREATE INDEX IF NOT EXISTS %s ON %s (event_type, timestamp_unix_nano DESC);
`, tableIdent, tenantIndexIdent, tableIdent, modelIndexIdent, tableIdent, eventIndexIdent, tableIdent)
	_, err := p.pg.Exec(ctx, q)
	return err
}

func indexTranscript(ctx context.Context, pg *pgxpool.Pool, table string, ev sdk.GatewayEvent, obj transcriptObject, bucket, key string, uncompressedBytes, storedBytes int, gzipEnabled bool) error {
	props, _ := json.Marshal(obj.Properties)
	errJSON, _ := json.Marshal(obj.Error)
	q := fmt.Sprintf(`
INSERT INTO %s (
  request_id, event_type, tenant_id, timestamp_unix_nano,
  provider, model, route_provider, route_model,
  input_tokens, output_tokens, total_tokens, cost_usd,
  bucket, object_key, uncompressed_bytes, stored_bytes, gzip,
  properties, error
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19::jsonb
)
ON CONFLICT (request_id, event_type) DO UPDATE SET
  stored_at = now(), tenant_id = excluded.tenant_id,
  provider = excluded.provider, model = excluded.model,
  route_provider = excluded.route_provider, route_model = excluded.route_model,
  input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens,
  total_tokens = excluded.total_tokens, cost_usd = excluded.cost_usd,
  bucket = excluded.bucket, object_key = excluded.object_key,
  uncompressed_bytes = excluded.uncompressed_bytes, stored_bytes = excluded.stored_bytes,
  gzip = excluded.gzip, properties = excluded.properties, error = excluded.error`, pgIdent(table))
	_, err := pg.Exec(ctx, q,
		ev.RequestID, ev.EventType, ev.TenantID, ev.TimestampUnixNano,
		ev.Usage.ProviderInstance, ev.Usage.ProviderModel, obj.Properties["route_provider"], obj.Properties["route_model"],
		ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.TotalTokens, ev.Usage.CostUSD,
		bucket, key, uncompressedBytes, storedBytes, gzipEnabled,
		string(props), string(errJSON),
	)
	return err
}

func pgIdent(s string) string {
	if s == "" {
		s = "vulpes_transcripts"
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func parseTenantSet(values ...any) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		switch v := value.(type) {
		case string:
			addTenants(out, v)
		case []any:
			for _, item := range v {
				addTenants(out, stringValue(item))
			}
		case []string:
			for _, item := range v {
				addTenants(out, item)
			}
		}
	}
	return out
}

func addTenants(out map[string]struct{}, raw string) {
	for _, item := range strings.Split(raw, ",") {
		tenantID := strings.TrimSpace(item)
		if tenantID != "" {
			out[tenantID] = struct{}{}
		}
	}
}

func copyTenantSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for tenantID := range in {
		out[tenantID] = struct{}{}
	}
	return out
}

func shouldBypassTenant(tenantID string, bypassTenantIDs map[string]struct{}) bool {
	if tenantID == "" || len(bypassTenantIDs) == 0 {
		return false
	}
	_, ok := bypassTenantIDs[tenantID]
	return ok
}

func buildTranscript(ev sdk.GatewayEvent) transcriptObject {
	props := map[string]string{}
	for k, v := range ev.Properties {
		if k == "request_json" || k == "response_json" {
			continue
		}
		props[k] = v
	}
	obj := transcriptObject{Version: "gateway.fenko.dev/transcript.v1", StoredAt: time.Now().UTC().Format(time.RFC3339Nano), RequestID: ev.RequestID, TenantID: ev.TenantID, EventType: ev.EventType, Timestamp: ev.TimestampUnixNano, Properties: props, Usage: ev.Usage, Error: ev.Error}
	if raw := ev.Properties["request_json"]; raw != "" && json.Valid([]byte(raw)) {
		obj.Request = json.RawMessage(raw)
	}
	if raw := ev.Properties["response_json"]; raw != "" && json.Valid([]byte(raw)) {
		obj.Response = json.RawMessage(raw)
	}
	return obj
}

func marshalTranscript(obj transcriptObject, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(obj, "", "  ")
	}
	return json.Marshal(obj)
}

func gzipBytes(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func transcriptKey(prefix string, ev sdk.GatewayEvent, gzipEnabled bool) string {
	t := time.Unix(0, ev.TimestampUnixNano).UTC()
	if ev.TimestampUnixNano == 0 {
		t = time.Now().UTC()
	}
	requestID := safePart(ev.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("event-%d", t.UnixNano())
	}
	suffix := ".json"
	if gzipEnabled {
		suffix = ".json.gz"
	}
	parts := []string{prefix, t.Format("2006/01/02/15"), requestID + "." + safePart(ev.EventType) + suffix}
	return strings.TrimPrefix(path.Join(parts...), "/")
}

func safePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func firstString(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-s3-transcripts",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "s3-transcripts", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{SecretNames: []string{"R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "TRANSCRIPTS_DATABASE_URL", "DATABASE_URL"}, Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}},
		},
		Schema:     `{"type":"object","required":["bucket"],"properties":{"bucket":{"type":"string"},"prefix":{"type":"string"},"endpoint":{"type":"string"},"region":{"type":"string"},"access_key_id":{"type":"string"},"secret_access_key":{"type":"string"},"session_token":{"type":"string"},"force_path_style":{"type":"boolean"},"store_all_events":{"type":"boolean"},"gzip":{"type":"boolean"},"pretty":{"type":"boolean"},"database_url":{"type":"string"},"postgres_url":{"type":"string"},"database_table":{"type":"string"},"database_auto_create":{"type":"boolean"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
