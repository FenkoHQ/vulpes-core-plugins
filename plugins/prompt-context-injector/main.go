package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type rule struct {
	Name     string
	Key      string
	Model    string
	Tenant   string
	Subject  string
	Mode     string
	Content  string
	Messages []sdk.ChatMessage
}

type plugin struct{ rules []rule }

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.rules = nil
	items, _ := cfg["rules"].([]any)
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		r := rule{Name: str(m["name"]), Key: str(m["key"]), Model: str(m["model"]), Tenant: str(m["tenant"]), Subject: str(m["subject"]), Mode: str(m["mode"]), Content: str(m["content"])}
		if r.Mode == "" {
			r.Mode = "prepend_system"
		}
		if raw, ok := m["messages"]; ok {
			r.Messages = parseMessages(raw)
		}
		p.rules = append(p.rules, r)
	}
	return nil
}

func (p *plugin) ResolvePrompt(ctx context.Context, req sdk.PromptResolveRequest) (sdk.PromptResolveResponse, error) {
	var current []sdk.ChatMessage
	_ = json.Unmarshal([]byte(req.Variables["messages_json"]), &current)
	model := req.Variables["model"]
	for _, r := range p.rules {
		if !matches(r.Key, req.PromptRef) || !matches(r.Model, model) || !matches(r.Tenant, req.Identity.TenantID) || !matches(r.Subject, req.Identity.Subject) {
			continue
		}
		messages := applyRule(current, r)
		return sdk.PromptResolveResponse{Messages: messages, ResolvedVersion: r.Name, Properties: map[string]string{"context_injected": "true", "context_rule": r.Name, "context_mode": r.Mode}}, nil
	}
	return sdk.PromptResolveResponse{}, nil
}

func applyRule(current []sdk.ChatMessage, r rule) []sdk.ChatMessage {
	inject := r.Messages
	if len(inject) == 0 && r.Content != "" {
		inject = []sdk.ChatMessage{{Role: "system", Content: r.Content}}
	}
	switch r.Mode {
	case "replace":
		return clone(inject)
	case "replace_system":
		out := make([]sdk.ChatMessage, 0, len(current)+len(inject))
		out = append(out, inject...)
		for _, m := range current {
			if !strings.EqualFold(m.Role, "system") {
				out = append(out, m)
			}
		}
		return out
	case "append":
		out := clone(current)
		return append(out, inject...)
	case "prepend":
		out := clone(inject)
		return append(out, current...)
	default: // prepend_system
		out := make([]sdk.ChatMessage, 0, len(current)+len(inject))
		out = append(out, inject...)
		out = append(out, current...)
		return out
	}
}

func matches(pattern, value string) bool { return pattern == "" || pattern == "*" || pattern == value }
func clone(in []sdk.ChatMessage) []sdk.ChatMessage {
	out := make([]sdk.ChatMessage, len(in))
	copy(out, in)
	return out
}
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func parseMessages(raw any) []sdk.ChatMessage {
	b, _ := json.Marshal(raw)
	var msgs []sdk.ChatMessage
	_ = json.Unmarshal(b, &msgs)
	return msgs
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata:       sdk.Metadata{Name: "prompt-context-injector", Version: "0.1.0", Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityPromptProvider, Name: "context-injector", Version: "0.1.0"}}, Permissions: sdk.Permissions{Data: sdk.DataPermissions{ReadPrompt: true, ModifyRequest: true}}},
		Schema:         `{"type":"object","properties":{"rules":{"type":"array"}}}`,
		Configurer:     p,
		PromptProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
