package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestConsulRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Service":{"ID":"llm1","Service":"litellm","Address":"10.0.0.1","Port":4000,"Tags":["model:gpt","provider_model:gpt-4o-mini"],"Meta":{}},"Node":{"Address":"10.0.0.1"}}]`))
	}))
	defer srv.Close()
	p := &plugin{}
	if err := p.Configure(context.Background(), map[string]any{"address": srv.URL, "provider_instance": "litellm"}, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := p.Route(context.Background(), sdk.RouteRequest{RequestedModel: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Routes) != 1 {
		t.Fatalf("routes = %d", len(resp.Routes))
	}
	if resp.Routes[0].Properties["base_url"] != "http://10.0.0.1:4000/v1" {
		t.Fatalf("bad base_url %#v", resp.Routes[0].Properties)
	}
}
