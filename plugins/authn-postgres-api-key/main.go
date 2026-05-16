package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
	"github.com/jackc/pgx/v5/pgxpool"
)

type plugin struct {
	pool       *pgxpool.Pool
	table      string
	autoCreate bool
}

type apiKeyRecord struct {
	KeyID      string
	Subject    string
	TenantID   string
	Groups     []string
	ClaimsJSON []byte
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	dsn := firstString(cfg["database_url"], cfg["dsn"], secrets["AUTH_DATABASE_URL"], secrets["DATABASE_URL"])
	if dsn == "" {
		return fmt.Errorf("database_url is required")
	}
	p.table = firstString(cfg["table"], "vulpes_api_keys")
	p.autoCreate = true
	if v, ok := cfg["auto_create"].(bool); ok {
		p.autoCreate = v
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return err
	}
	p.pool = pool
	if p.autoCreate {
		return p.createSchema(ctx)
	}
	return nil
}

func (p *plugin) Authenticate(ctx context.Context, req sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	if p.pool == nil {
		return sdk.AuthenticateResponse{Allow: false, DenyReason: "auth database not configured"}, nil
	}
	token := extractToken(req.Headers)
	if token == "" {
		return sdk.AuthenticateResponse{Allow: false, DenyReason: "missing API key"}, nil
	}
	rec, storedHash, err := p.lookup(ctx, token)
	if err != nil {
		return sdk.AuthenticateResponse{}, err
	}
	if rec.KeyID == "" || subtle.ConstantTimeCompare([]byte(storedHash), []byte(keyHash(token))) != 1 {
		return sdk.AuthenticateResponse{Allow: false, DenyReason: "invalid API key"}, nil
	}
	claims := map[string]string{"key_id": rec.KeyID}
	return sdk.AuthenticateResponse{Allow: true, Identity: sdk.Identity{Subject: rec.Subject, TenantID: rec.TenantID, Groups: rec.Groups, Claims: claims, AuthMethod: "postgres_api_key"}}, nil
}

func (p *plugin) lookup(ctx context.Context, token string) (apiKeyRecord, string, error) {
	h := keyHash(token)
	q := fmt.Sprintf(`
SELECT key_id, key_hash, subject, tenant_id, COALESCE(groups, ARRAY[]::text[])
FROM %s
WHERE key_hash = $1
  AND active = true
  AND (expires_at IS NULL OR expires_at > now())
LIMIT 1`, pgIdent(p.table))
	var rec apiKeyRecord
	var storedHash string
	err := p.pool.QueryRow(ctx, q, h).Scan(&rec.KeyID, &storedHash, &rec.Subject, &rec.TenantID, &rec.Groups)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return apiKeyRecord{}, "", nil
		}
		return apiKeyRecord{}, "", err
	}
	return rec, storedHash, nil
}

func (p *plugin) createSchema(ctx context.Context) error {
	table := pgIdent(p.table)
	q := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
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
CREATE INDEX IF NOT EXISTS %s ON %s (key_hash) WHERE active = true;
CREATE INDEX IF NOT EXISTS %s ON %s (tenant_id);
`, table, pgIdent(p.table+"_key_hash_active_idx"), table, pgIdent(p.table+"_tenant_idx"), table)
	_, err := p.pool.Exec(ctx, q)
	return err
}

func extractToken(headers map[string]string) string {
	for _, name := range []string{"Authorization", "authorization"} {
		if token := bearer(headers[name]); token != "" {
			return token
		}
	}
	for _, name := range []string{"X-API-Key", "X-Api-Key", "x-api-key"} {
		if headers[name] != "" {
			return strings.TrimSpace(headers[name])
		}
	}
	return ""
}

func bearer(v string) string {
	parts := strings.Fields(v)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func keyHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func pgIdent(s string) string {
	if s == "" {
		s = "vulpes_api_keys"
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "authn-postgres-api-key",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityAuthenticator, Name: "postgres-api-key", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{SecretNames: []string{"AUTH_DATABASE_URL", "DATABASE_URL"}, Data: sdk.DataPermissions{ReadHeaders: true}},
		},
		Schema:        `{"type":"object","required":["database_url"],"properties":{"database_url":{"type":"string"},"dsn":{"type":"string"},"table":{"type":"string"},"auto_create":{"type":"boolean"}}}`,
		Configurer:    p,
		Authenticator: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
