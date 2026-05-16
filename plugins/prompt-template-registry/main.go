package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type prompt struct {
	Name     string
	Ref      string
	Version  string
	Default  bool
	Tenant   string
	Subject  string
	Model    string
	Mode     string
	Messages []sdk.ChatMessage
	Props    map[string]string
}

type plugin struct{ prompts []prompt }

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.prompts = nil
	items, _ := cfg["prompts"].([]any)
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pr := prompt{Name: str(m["name"]), Ref: str(m["ref"]), Version: str(m["version"]), Default: boolv(m["default"]), Tenant: str(m["tenant"]), Subject: str(m["subject"]), Model: str(m["model"]), Mode: str(m["mode"]), Messages: parseMessages(m["messages"]), Props: parseProps(m["properties"])}
		if pr.Ref == "" {
			pr.Ref = pr.Name
		}
		if pr.Version == "" {
			pr.Version = "v1"
		}
		if pr.Mode == "" {
			pr.Mode = "replace"
		}
		if len(pr.Messages) == 0 && str(m["content"]) != "" {
			pr.Messages = []sdk.ChatMessage{{Role: "system", Content: str(m["content"])}}
		}
		p.prompts = append(p.prompts, pr)
	}
	return nil
}

func (p *plugin) ResolvePrompt(ctx context.Context, req sdk.PromptResolveRequest) (sdk.PromptResolveResponse, error) {
	current := currentMessages(req)
	pr, ok := p.selectPrompt(req)
	if !ok {
		return sdk.PromptResolveResponse{}, nil
	}
	rendered := renderMessages(pr.Messages, req.Variables)
	props := map[string]string{"prompt_ref": pr.Ref, "prompt_version": pr.Version, "prompt_mode": pr.Mode}
	if pr.Name != "" {
		props["prompt_name"] = pr.Name
	}
	for k, v := range pr.Props {
		props[k] = render(v, req.Variables)
	}
	return sdk.PromptResolveResponse{Messages: applyMode(current, rendered, pr.Mode), ResolvedVersion: pr.Version, Properties: props}, nil
}

func (p *plugin) selectPrompt(req sdk.PromptResolveRequest) (prompt, bool) {
	model := req.Variables["model"]
	var matches []prompt
	for _, pr := range p.prompts {
		if !match(pr.Ref, req.PromptRef) || !match(pr.Tenant, req.Identity.TenantID) || !match(pr.Subject, req.Identity.Subject) || !match(pr.Model, model) {
			continue
		}
		if req.Version != "" && pr.Version != req.Version {
			continue
		}
		matches = append(matches, pr)
	}
	if len(matches) == 0 {
		return prompt{}, false
	}
	if req.Version == "" {
		for _, pr := range matches {
			if pr.Default {
				return pr, true
			}
		}
		sort.SliceStable(matches, func(i, j int) bool { return matches[i].Version > matches[j].Version })
	}
	return matches[0], true
}

func applyMode(current, inject []sdk.ChatMessage, mode string) []sdk.ChatMessage {
	switch mode {
	case "prepend_system":
		out := clone(inject)
		return append(out, current...)
	case "replace_system":
		out := clone(inject)
		for _, m := range current {
			if !strings.EqualFold(m.Role, "system") {
				out = append(out, m)
			}
		}
		return out
	case "prepend":
		out := clone(inject)
		return append(out, current...)
	case "append":
		out := clone(current)
		return append(out, inject...)
	default: // replace
		return clone(inject)
	}
}

func currentMessages(req sdk.PromptResolveRequest) []sdk.ChatMessage {
	var current []sdk.ChatMessage
	_ = json.Unmarshal([]byte(req.Variables["messages_json"]), &current)
	return current
}
func renderMessages(in []sdk.ChatMessage, vars map[string]string) []sdk.ChatMessage {
	out := clone(in)
	for i := range out {
		out[i].Content = renderAny(out[i].Content, vars)
		out[i].Name = render(out[i].Name, vars)
		out[i].ToolCallID = render(out[i].ToolCallID, vars)
	}
	return out
}
func renderAny(v any, vars map[string]string) any {
	switch x := v.(type) {
	case string:
		return render(x, vars)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = renderAny(x[i], vars)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = renderAny(v, vars)
		}
		return out
	default:
		return v
	}
}
func render(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}
func parseMessages(raw any) []sdk.ChatMessage {
	b, _ := json.Marshal(raw)
	var msgs []sdk.ChatMessage
	_ = json.Unmarshal(b, &msgs)
	return msgs
}
func parseProps(raw any) map[string]string {
	out := map[string]string{}
	m, _ := raw.(map[string]any)
	for k, v := range m {
		out[k] = str(v)
	}
	return out
}
func clone(in []sdk.ChatMessage) []sdk.ChatMessage {
	out := make([]sdk.ChatMessage, len(in))
	copy(out, in)
	return out
}
func match(pattern, value string) bool { return pattern == "" || pattern == "*" || pattern == value }
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func boolv(v any) bool {
	b, _ := v.(bool)
	return b
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata:       sdk.Metadata{Name: "prompt-template-registry", Version: "0.1.0", Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityPromptProvider, Name: "prompt-template-registry", Version: "0.1.0"}}, Permissions: sdk.Permissions{Data: sdk.DataPermissions{ReadPrompt: true, ModifyRequest: true}}},
		Schema:         `{"type":"object","properties":{"prompts":{"type":"array"}}}`,
		Configurer:     p,
		PromptProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
