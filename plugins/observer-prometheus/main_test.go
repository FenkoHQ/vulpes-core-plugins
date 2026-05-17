package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestMetricsRender(t *testing.T) {
	p := newPlugin()
	if err := p.Emit(context.Background(), []sdk.GatewayEvent{
		{EventType: "request.completed", TenantID: "tenant-a", Usage: sdk.Usage{ProviderInstance: "openai", ProviderModel: "gpt-4o-mini", InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostUSD: 0.001}},
		{EventType: "cache.hit", TenantID: "tenant-a"},
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`vulpes_gateway_events_total{event_type="request.completed"} 1`,
		`vulpes_gateway_events_total{event_type="cache.hit"} 1`,
		`vulpes_gateway_requests_total{status="completed"} 1`,
		`vulpes_gateway_usage_input_tokens_total{model="gpt-4o-mini",provider="openai"} 10`,
		`vulpes_gateway_usage_cost_usd_total{model="gpt-4o-mini",provider="openai"} 0.001`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q\n%s", want, body)
		}
	}
}

func TestModelCatalogMetric(t *testing.T) {
	p := newPlugin()
	if err := p.Configure(context.Background(), map[string]any{
		"model_catalog": []any{
			map[string]any{
				"model":          "gpt-5.5",
				"provider":       "codex",
				"upstream_model": "gpt-5.5",
				"base_url":       "https://chatgpt.com/backend-api/codex",
				"endpoint":       "http://vulpes-gateway.service.den.internal:18088/v1/chat/completions",
			},
		},
	}, nil); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	want := `vulpes_gateway_model_info{base_url="https://chatgpt.com/backend-api/codex",endpoint="http://vulpes-gateway.service.den.internal:18088/v1/chat/completions",model="gpt-5.5",provider="codex",upstream_model="gpt-5.5"} 1`
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("model catalog metric missing %q\n%s", want, rec.Body.String())
	}
}

func TestTenantLabelsOptIn(t *testing.T) {
	p := newPlugin()
	p.enableTenantLabels = true
	if err := p.Emit(context.Background(), []sdk.GatewayEvent{{EventType: "request.failed", TenantID: "tenant-a"}}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.handleMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `tenant_id="tenant-a"`) {
		t.Fatalf("tenant label missing:\n%s", rec.Body.String())
	}
}
