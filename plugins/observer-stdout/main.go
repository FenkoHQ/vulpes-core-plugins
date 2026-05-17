package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	pretty          bool
	includePayloads bool
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.includePayloads = true
	if v, ok := cfg["pretty"].(bool); ok {
		p.pretty = v
	}
	if v, ok := cfg["include_payloads"].(bool); ok {
		p.includePayloads = v
	}
	if v, ok := cfg["omit_payloads"].(bool); ok && v {
		p.includePayloads = false
	}
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	enc := json.NewEncoder(os.Stdout)
	if p.pretty {
		enc.SetIndent("", "  ")
	}
	for _, ev := range events {
		if !p.includePayloads {
			ev = omitPayloads(ev)
		}
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

func omitPayloads(ev sdk.GatewayEvent) sdk.GatewayEvent {
	if len(ev.Properties) == 0 {
		return ev
	}
	props := make(map[string]string, len(ev.Properties)+2)
	for k, v := range ev.Properties {
		switch k {
		case "request_json":
			props["request_json_omitted"] = "true"
		case "response_json":
			props["response_json_omitted"] = "true"
		default:
			props[k] = v
		}
	}
	ev.Properties = props
	return ev
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-stdout",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "stdout", Version: "0.1.0"}},
		},
		Schema:     `{"type":"object","properties":{"pretty":{"type":"boolean"},"include_payloads":{"type":"boolean"},"omit_payloads":{"type":"boolean"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
