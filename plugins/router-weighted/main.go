package main

import (
	"context"
	"math/rand/v2"
	"sort"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct{ strategy string }

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.strategy = "weighted"
	if s, ok := cfg["strategy"].(string); ok && s != "" {
		p.strategy = s
	}
	return nil
}

func (p *plugin) Route(ctx context.Context, req sdk.RouteRequest) (sdk.RouteResponse, error) {
	candidates := make([]sdk.RouteCandidate, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		if c.Healthy {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return sdk.RouteResponse{Reason: "no healthy candidates"}, nil
	}

	switch p.strategy {
	case "ordered", "fallback":
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Weight > candidates[j].Weight })
	case "shuffle":
		rand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	default:
		candidates = weightedOrder(candidates)
	}

	routes := make([]sdk.SelectedRoute, 0, len(candidates))
	for i, c := range candidates {
		routes = append(routes, sdk.SelectedRoute{ProviderInstance: c.ProviderInstance, ProviderModel: c.ProviderModel, Priority: i, Properties: c.Properties})
	}
	return sdk.RouteResponse{Routes: routes, Reason: p.strategy}, nil
}

func weightedOrder(in []sdk.RouteCandidate) []sdk.RouteCandidate {
	remaining := append([]sdk.RouteCandidate(nil), in...)
	out := make([]sdk.RouteCandidate, 0, len(remaining))
	for len(remaining) > 0 {
		total := 0
		for _, c := range remaining {
			w := c.Weight
			if w <= 0 {
				w = 1
			}
			total += w
		}
		pick := rand.IntN(total)
		acc := 0
		idx := 0
		for i, c := range remaining {
			w := c.Weight
			if w <= 0 {
				w = 1
			}
			acc += w
			if pick < acc {
				idx = i
				break
			}
		}
		out = append(out, remaining[idx])
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return out
}

func main() {
	p := &plugin{strategy: "weighted"}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "router-weighted",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityRouter, Name: "weighted", Version: "0.1.0"}},
		},
		Schema:     `{"type":"object","properties":{"strategy":{"type":"string","enum":["weighted","shuffle","ordered","fallback"]}}}`,
		Configurer: p,
		Router:     p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
