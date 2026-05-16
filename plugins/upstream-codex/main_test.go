package main

import (
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestMessagesToResponsesInput(t *testing.T) {
	got := messagesToResponsesInput([]sdk.ChatMessage{{Role: "user", Content: "hello"}})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0]["role"] != "user" {
		t.Fatalf("role = %v", got[0]["role"])
	}
	content, ok := got[0]["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", got[0]["content"])
	}
	if content[0]["type"] != "input_text" || content[0]["text"] != "hello" {
		t.Fatalf("content[0] = %#v", content[0])
	}
}

func TestCollectOutputText(t *testing.T) {
	out := []responsesOutputItem{{Content: []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "output_text", Text: "hello"}, {Type: "output_text", Text: " world"}}}}
	if got := collectOutputText(out); got != "hello world" {
		t.Fatalf("text = %q", got)
	}
}

func TestEstimateCostUSD(t *testing.T) {
	usage := sdk.Usage{InputTokens: 1000, OutputTokens: 500}
	got := estimateCostUSD(map[string]string{"input_cost_per_1k_tokens": "0.001", "output_cost_per_1k_tokens": "0.002"}, usage)
	if got != 0.002 {
		t.Fatalf("cost = %v, want 0.002", got)
	}
}
