package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct{ pretty bool }

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	if v, ok := cfg["pretty"].(bool); ok {
		p.pretty = v
	}
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	enc := json.NewEncoder(os.Stdout)
	if p.pretty {
		enc.SetIndent("", "  ")
	}
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-stdout",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "stdout", Version: "0.1.0"}},
		},
		Schema:     `{"type":"object","properties":{"pretty":{"type":"boolean"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
