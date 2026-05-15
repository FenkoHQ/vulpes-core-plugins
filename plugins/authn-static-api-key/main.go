package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type keyConfig struct {
	ID      string `json:"id"`
	Value   string `json:"value"`
	Tenant  string `json:"tenant"`
	Subject string `json:"subject"`
}

type plugin struct{ keys []keyConfig }

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.keys = nil
	raw, ok := cfg["keys"].([]any)
	if !ok || len(raw) == 0 {
		return fmt.Errorf("keys must be a non-empty array")
	}
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("keys[%d] must be an object", i)
		}
		k := keyConfig{ID: stringValue(m["id"]), Value: stringValue(m["value"]), Tenant: stringValue(m["tenant"]), Subject: stringValue(m["subject"])}
		if k.ID == "" {
			k.ID = fmt.Sprintf("key-%d", i)
		}
		if k.Value == "" {
			return fmt.Errorf("keys[%d].value is required", i)
		}
		if k.Tenant == "" {
			k.Tenant = k.ID
		}
		if k.Subject == "" {
			k.Subject = k.ID
		}
		p.keys = append(p.keys, k)
	}
	return nil
}

func (p *plugin) Authenticate(ctx context.Context, req sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	token := bearer(req.Headers["Authorization"])
	if token == "" {
		token = req.Headers["X-API-Key"]
	}
	if token == "" {
		token = req.Headers["X-Api-Key"]
	}
	if token == "" {
		return sdk.AuthenticateResponse{Allow: false, DenyReason: "missing API key"}, nil
	}
	for _, k := range p.keys {
		if subtle.ConstantTimeCompare([]byte(token), []byte(k.Value)) == 1 {
			return sdk.AuthenticateResponse{Allow: true, Identity: sdk.Identity{Subject: k.Subject, TenantID: k.Tenant, AuthMethod: "static_api_key", Claims: map[string]string{"key_id": k.ID}}}, nil
		}
	}
	return sdk.AuthenticateResponse{Allow: false, DenyReason: "invalid API key"}, nil
}

func bearer(v string) string {
	parts := strings.Fields(v)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "authn-static-api-key",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityAuthenticator, Name: "static-api-key", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{SecretNames: []string{"GATEWAY_API_KEY", "DEV_API_KEY", "VULPES_API_KEY"}, Data: sdk.DataPermissions{ReadHeaders: true}},
		},
		Schema:        `{"type":"object","required":["keys"],"properties":{"keys":{"type":"array","minItems":1,"items":{"type":"object","required":["value"],"properties":{"id":{"type":"string"},"value":{"type":"string"},"tenant":{"type":"string"},"subject":{"type":"string"}}}}}}`,
		Configurer:    p,
		Authenticator: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
