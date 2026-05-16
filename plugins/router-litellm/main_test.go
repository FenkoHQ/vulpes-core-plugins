package main

import (
	"context"
	"testing"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestFallbackModelOrder(t *testing.T) {
	p := newPlugin()
	cfg := map[string]any{
		"routing_strategy": "ordered",
		"model_list": []any{
			map[string]any{"model_name": "gpt-4", "provider_instance": "openai-a", "model": "gpt-4", "weight": float64(10)},
			map[string]any{"model_name": "gpt-4o-mini", "provider_instance": "openai-b", "model": "gpt-4o-mini", "weight": float64(100)},
		},
		"fallbacks": map[string]any{"gpt-4": []any{"gpt-4o-mini"}},
	}
	if err := p.Configure(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r1"}, RequestedModel: "gpt-4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Routes) != 2 {
		t.Fatalf("routes = %d", len(resp.Routes))
	}
	if resp.Routes[0].ProviderInstance != "openai-a" || resp.Routes[1].ProviderInstance != "openai-b" {
		t.Fatalf("bad route order: %#v", resp.Routes)
	}
}

func TestLeastBusy(t *testing.T) {
	p := newPlugin()
	cfg := map[string]any{
		"routing_strategy": "least-busy",
		"model_list": []any{
			map[string]any{"model_name": "gpt", "provider_instance": "a", "model": "m"},
			map[string]any{"model_name": "gpt", "provider_instance": "b", "model": "m"},
		},
	}
	if err := p.Configure(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	p.deployments[0].InFlight = 10
	resp, err := p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r1"}, RequestedModel: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Routes[0].ProviderInstance != "b" {
		t.Fatalf("expected b first, got %#v", resp.Routes[0])
	}
}

func TestCooldownAfterFailures(t *testing.T) {
	p := newPlugin()
	cfg := map[string]any{
		"routing_strategy":      "ordered",
		"allowed_fails":         float64(1),
		"cooldown_time_seconds": float64(60),
		"model_list": []any{
			map[string]any{"model_name": "gpt", "provider_instance": "a", "model": "m", "weight": float64(100)},
			map[string]any{"model_name": "gpt", "provider_instance": "b", "model": "m", "weight": float64(1)},
		},
	}
	if err := p.Configure(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Emit(context.Background(), []sdk.GatewayEvent{{EventType: "upstream.error", Properties: map[string]string{"route_provider": "a", "route_model": "m"}}}); err != nil {
		t.Fatal(err)
	}
	if !p.deployments[0].CooldownUntil.After(time.Now()) {
		t.Fatal("deployment a was not cooled down")
	}
	resp, err := p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r2"}, RequestedModel: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Routes) == 0 || resp.Routes[0].ProviderInstance != "b" {
		t.Fatalf("expected b after cooldown, got %#v", resp.Routes)
	}
}

func TestRouteCarriesCostProperties(t *testing.T) {
	p := newPlugin()
	cfg := map[string]any{
		"routing_strategy": "ordered",
		"model_list": []any{
			map[string]any{
				"model_name":        "gpt",
				"provider_instance": "openai-a",
				"model":             "m",
				"model_info": map[string]any{
					"input_cost_per_token":  float64(0.000001),
					"output_cost_per_token": float64(0.000002),
				},
			},
		},
	}
	if err := p.Configure(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r1"}, RequestedModel: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	props := resp.Routes[0].Properties
	if props["input_cost_per_1k_tokens"] != "0.001" || props["output_cost_per_1k_tokens"] != "0.002" {
		t.Fatalf("missing cost props: %#v", props)
	}
}

func TestRPMFiltering(t *testing.T) {
	p := newPlugin()
	cfg := map[string]any{
		"routing_strategy": "ordered",
		"model_list": []any{
			map[string]any{"model_name": "gpt", "provider_instance": "a", "model": "m", "rpm": float64(1), "weight": float64(100)},
			map[string]any{"model_name": "gpt", "provider_instance": "b", "model": "m", "weight": float64(1)},
		},
	}
	if err := p.Configure(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r1"}, RequestedModel: "gpt"})
	resp, err := p.Route(context.Background(), sdk.RouteRequest{Context: sdk.CallContext{RequestID: "r2"}, RequestedModel: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Routes) == 0 || resp.Routes[0].ProviderInstance != "b" {
		t.Fatalf("expected b after rpm filter, got %#v", resp.Routes)
	}
}
