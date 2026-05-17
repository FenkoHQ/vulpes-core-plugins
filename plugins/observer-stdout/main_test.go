package main

import (
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestOmitPayloadsKeepsLookupFields(t *testing.T) {
	ev := sdk.GatewayEvent{
		RequestID: "req_1",
		TenantID:  "tenant_1",
		EventType: "request.completed",
		Properties: map[string]string{
			"requested_model": "frugal",
			"route_provider":  "frugalai",
			"request_json":    `{"messages":[{"content":"secret"}]}`,
			"response_json":   `{"choices":[{"message":{"content":"secret"}}]}`,
		},
	}

	got := omitPayloads(ev)
	if got.RequestID != ev.RequestID || got.TenantID != ev.TenantID || got.EventType != ev.EventType {
		t.Fatalf("lookup fields changed: %#v", got)
	}
	if _, ok := got.Properties["request_json"]; ok {
		t.Fatal("request_json was not omitted")
	}
	if _, ok := got.Properties["response_json"]; ok {
		t.Fatal("response_json was not omitted")
	}
	if got.Properties["request_json_omitted"] != "true" || got.Properties["response_json_omitted"] != "true" {
		t.Fatalf("missing omission markers: %#v", got.Properties)
	}
	if got.Properties["route_provider"] != "frugalai" {
		t.Fatalf("route metadata was not preserved: %#v", got.Properties)
	}
}
