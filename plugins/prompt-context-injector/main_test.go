package main

import (
	"context"
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestInjectByKey(t *testing.T) {
	p := &plugin{}
	if err := p.Configure(context.Background(), map[string]any{"rules": []any{map[string]any{"name": "dev", "key": "dev", "mode": "prepend_system", "content": "You are terse."}}}, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := p.ResolvePrompt(context.Background(), sdk.PromptResolveRequest{PromptRef: "dev", Identity: sdk.Identity{TenantID: "t"}, Variables: map[string]string{"messages_json": `[{"role":"user","content":"hi"}]`}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Messages) != 2 || resp.Messages[0].Role != "system" {
		t.Fatalf("bad messages %#v", resp.Messages)
	}
}

func TestReplaceSystemByModel(t *testing.T) {
	p := &plugin{}
	_ = p.Configure(context.Background(), map[string]any{"rules": []any{map[string]any{"name": "m", "model": "gpt", "mode": "replace_system", "content": "new"}}}, nil)
	resp, err := p.ResolvePrompt(context.Background(), sdk.PromptResolveRequest{PromptRef: "gpt", Variables: map[string]string{"model": "gpt", "messages_json": `[{"role":"system","content":"old"},{"role":"user","content":"hi"}]`}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Messages) != 2 || resp.Messages[0].Content != "new" {
		t.Fatalf("bad messages %#v", resp.Messages)
	}
}
