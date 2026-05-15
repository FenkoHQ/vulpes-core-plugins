package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestBuildTranscriptExtractsPayloads(t *testing.T) {
	ev := sdk.GatewayEvent{
		RequestID: "req_1",
		TenantID:  "tenant",
		EventType: "request.completed",
		Properties: map[string]string{
			"route_provider": "openai",
			"request_json":   `{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`,
			"response_json":  `{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`,
		},
	}
	obj := buildTranscript(ev)
	if !json.Valid(obj.Request) || !json.Valid(obj.Response) {
		t.Fatalf("payloads not extracted: %#v", obj)
	}
	if _, ok := obj.Properties["request_json"]; ok {
		t.Fatal("raw request_json leaked into properties")
	}
}

func TestTranscriptKey(t *testing.T) {
	ts := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC).UnixNano()
	key := transcriptKey("logs/transcripts", sdk.GatewayEvent{RequestID: "req/1", EventType: "request.completed", TimestampUnixNano: ts})
	if !strings.HasPrefix(key, "logs/transcripts/2026/05/16/12/") {
		t.Fatalf("bad key prefix: %s", key)
	}
	if !strings.Contains(key, "req_1.request.completed.json") {
		t.Fatalf("bad key: %s", key)
	}
}
