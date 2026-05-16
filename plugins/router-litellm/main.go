package main

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	mu sync.Mutex

	strategy               string
	allowedFails           int
	failureWindow          time.Duration
	cooldownTime           time.Duration
	respectRPMTPM          bool
	fallbacks              map[string][]string
	contextFallbacks       map[string][]string
	contentPolicyFallbacks map[string][]string
	deployments            []deployment
	inflightByRequest      map[string]string
}

type deployment struct {
	Key                   string
	ModelName             string
	ProviderInstance      string
	ProviderModel         string
	Weight                int
	RPM                   int64
	TPM                   int64
	Region                string
	ContextWindow         int64
	InputCostPer1KTokens  float64
	OutputCostPer1KTokens float64
	Properties            map[string]string

	InFlight      int64
	CooldownUntil time.Time
	Failures      []time.Time
	RequestTimes  []time.Time
	TokenEvents   []tokenEvent
	LatencyEWMA   float64
}

type tokenEvent struct {
	At     time.Time
	Tokens int64
}

func newPlugin() *plugin {
	return &plugin{
		strategy:               "simple-shuffle",
		allowedFails:           5,
		failureWindow:          60 * time.Second,
		cooldownTime:           60 * time.Second,
		respectRPMTPM:          true,
		fallbacks:              map[string][]string{},
		contextFallbacks:       map[string][]string{},
		contentPolicyFallbacks: map[string][]string{},
		inflightByRequest:      map[string]string{},
	}
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if s := stringValue(cfg["routing_strategy"]); s != "" {
		p.strategy = s
	} else if s := stringValue(cfg["strategy"]); s != "" {
		p.strategy = s
	}
	if n := intValue(cfg["allowed_fails"]); n > 0 {
		p.allowedFails = int(n)
	}
	if n := intValue(cfg["failure_window_seconds"]); n > 0 {
		p.failureWindow = time.Duration(n) * time.Second
	}
	if n := intValue(cfg["cooldown_time_seconds"]); n > 0 {
		p.cooldownTime = time.Duration(n) * time.Second
	}
	if v, ok := cfg["respect_rpm_tpm"].(bool); ok {
		p.respectRPMTPM = v
	}
	p.fallbacks = parseFallbacks(cfg["fallbacks"])
	p.contextFallbacks = parseFallbacks(cfg["context_window_fallbacks"])
	p.contentPolicyFallbacks = parseFallbacks(cfg["content_policy_fallbacks"])

	p.deployments = nil
	if raw, ok := cfg["model_list"]; ok {
		p.deployments = parseDeployments(raw)
	} else if raw, ok := cfg["deployments"]; ok {
		p.deployments = parseDeployments(raw)
	}
	return nil
}

func (p *plugin) Route(ctx context.Context, req sdk.RouteRequest) (sdk.RouteResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(time.Now())

	deployments := p.candidateDeployments(req)
	if len(deployments) == 0 {
		return sdk.RouteResponse{Reason: "no deployments for requested model"}, nil
	}

	ordered := p.order(deployments)
	if len(ordered) == 0 {
		return sdk.RouteResponse{Reason: "all deployments are cooling down or over rpm/tpm limits"}, nil
	}

	requestID := req.Context.RequestID
	now := time.Now()
	if requestID != "" && len(ordered) > 0 {
		idx := p.findDeployment(ordered[0].Key)
		if idx >= 0 {
			p.deployments[idx].InFlight++
			p.deployments[idx].RequestTimes = append(p.deployments[idx].RequestTimes, now)
			p.inflightByRequest[requestID] = ordered[0].Key
		}
	}

	routes := make([]sdk.SelectedRoute, 0, len(ordered))
	for i, d := range ordered {
		props := cloneProps(d.Properties)
		props["litellm_model_group"] = d.ModelName
		props["litellm_routing_strategy"] = p.strategy
		props["litellm_deployment_key"] = d.Key
		if d.InputCostPer1KTokens > 0 {
			props["input_cost_per_1k_tokens"] = strconv.FormatFloat(d.InputCostPer1KTokens, 'f', -1, 64)
			props["estimated_cost_per_1k_input"] = props["input_cost_per_1k_tokens"]
		}
		if d.OutputCostPer1KTokens > 0 {
			props["output_cost_per_1k_tokens"] = strconv.FormatFloat(d.OutputCostPer1KTokens, 'f', -1, 64)
			props["estimated_cost_per_1k_output"] = props["output_cost_per_1k_tokens"]
		}
		routes = append(routes, sdk.SelectedRoute{ProviderInstance: d.ProviderInstance, ProviderModel: d.ProviderModel, Priority: i, Properties: props})
	}
	return sdk.RouteResponse{Routes: routes, Reason: "litellm_" + p.strategy}, nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for _, ev := range events {
		key := p.inflightByRequest[ev.RequestID]
		if key == "" {
			key = deploymentKey(ev.Properties["route_provider"], ev.Properties["route_model"])
		}
		idx := p.findDeployment(key)
		if idx < 0 {
			continue
		}
		d := &p.deployments[idx]
		switch ev.EventType {
		case "request.completed":
			if d.InFlight > 0 {
				d.InFlight--
			}
			delete(p.inflightByRequest, ev.RequestID)
			if ev.Usage.TotalTokens > 0 {
				d.TokenEvents = append(d.TokenEvents, tokenEvent{At: now, Tokens: ev.Usage.TotalTokens})
			}
			if ms := parseFloat(ev.Properties["duration_ms"]); ms > 0 {
				if d.LatencyEWMA == 0 {
					d.LatencyEWMA = ms
				} else {
					d.LatencyEWMA = 0.8*d.LatencyEWMA + 0.2*ms
				}
			}
		case "request.failed", "upstream.error":
			if d.InFlight > 0 && ev.EventType == "request.failed" {
				d.InFlight--
				delete(p.inflightByRequest, ev.RequestID)
			}
			d.Failures = append(d.Failures, now)
			if len(d.Failures) >= p.allowedFails {
				d.CooldownUntil = now.Add(p.cooldownTime)
			}
		}
	}
	p.prune(now)
	return nil
}

func (p *plugin) candidateDeployments(req sdk.RouteRequest) []deployment {
	modelOrder := p.modelOrder(req.RequestedModel)
	modelRank := map[string]int{}
	for i, m := range modelOrder {
		modelRank[m] = i
	}
	var source []deployment
	if len(p.deployments) > 0 {
		source = append([]deployment(nil), p.deployments...)
	} else {
		for _, c := range req.Candidates {
			source = append(source, deployment{Key: deploymentKey(c.ProviderInstance, c.ProviderModel), ModelName: c.LogicalModel, ProviderInstance: c.ProviderInstance, ProviderModel: c.ProviderModel, Weight: c.Weight, Region: c.Region, Properties: c.Properties})
		}
	}
	var out []deployment
	now := time.Now()
	for _, d := range source {
		if _, ok := modelRank[d.ModelName]; !ok {
			continue
		}
		if d.CooldownUntil.After(now) {
			continue
		}
		if p.respectRPMTPM && (d.RPM > 0 || d.TPM > 0) {
			idx := p.findDeployment(d.Key)
			if idx >= 0 && p.overLimit(p.deployments[idx]) {
				continue
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return modelRank[out[i].ModelName] < modelRank[out[j].ModelName] })
	return out
}

func (p *plugin) modelOrder(requested string) []string {
	seen := map[string]bool{requested: true}
	out := []string{requested}
	for _, fbMap := range []map[string][]string{p.fallbacks, p.contextFallbacks, p.contentPolicyFallbacks} {
		for _, m := range fbMap[requested] {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func (p *plugin) order(in []deployment) []deployment {
	groups := groupByModelPreserveOrder(in)
	var out []deployment
	for _, group := range groups {
		switch p.strategy {
		case "least-busy":
			sort.SliceStable(group, func(i, j int) bool {
				if group[i].InFlight == group[j].InFlight {
					return normalizedLatency(group[i]) < normalizedLatency(group[j])
				}
				return group[i].InFlight < group[j].InFlight
			})
		case "usage-based-routing":
			sort.SliceStable(group, func(i, j int) bool { return p.recentTokens(group[i]) < p.recentTokens(group[j]) })
		case "latency-based-routing":
			sort.SliceStable(group, func(i, j int) bool { return normalizedLatency(group[i]) < normalizedLatency(group[j]) })
		case "weighted", "simple-shuffle":
			group = weightedOrder(group)
		case "fallback", "ordered":
			sort.SliceStable(group, func(i, j int) bool { return group[i].Weight > group[j].Weight })
		default:
			group = weightedOrder(group)
		}
		out = append(out, group...)
	}
	return out
}

func groupByModelPreserveOrder(in []deployment) [][]deployment {
	var groups [][]deployment
	index := map[string]int{}
	for _, d := range in {
		idx, ok := index[d.ModelName]
		if !ok {
			index[d.ModelName] = len(groups)
			groups = append(groups, nil)
			idx = len(groups) - 1
		}
		groups[idx] = append(groups[idx], d)
	}
	return groups
}

func weightedOrder(in []deployment) []deployment {
	remaining := append([]deployment(nil), in...)
	out := make([]deployment, 0, len(remaining))
	for len(remaining) > 0 {
		total := 0
		for _, d := range remaining {
			w := d.Weight
			if w <= 0 {
				w = 1
			}
			total += w
		}
		pick := rand.IntN(total)
		acc, idx := 0, 0
		for i, d := range remaining {
			w := d.Weight
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

func (p *plugin) overLimit(d deployment) bool {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	reqs := int64(0)
	for _, t := range d.RequestTimes {
		if t.After(cutoff) {
			reqs++
		}
	}
	if d.RPM > 0 && reqs >= d.RPM {
		return true
	}
	tokens := int64(0)
	for _, ev := range d.TokenEvents {
		if ev.At.After(cutoff) {
			tokens += ev.Tokens
		}
	}
	return d.TPM > 0 && tokens >= d.TPM
}

func (p *plugin) prune(now time.Time) {
	failureCutoff := now.Add(-p.failureWindow)
	minuteCutoff := now.Add(-time.Minute)
	for i := range p.deployments {
		d := &p.deployments[i]
		d.Failures = filterTimes(d.Failures, failureCutoff)
		d.RequestTimes = filterTimes(d.RequestTimes, minuteCutoff)
		d.TokenEvents = filterTokenEvents(d.TokenEvents, minuteCutoff)
	}
}

func (p *plugin) recentTokens(d deployment) int64 {
	idx := p.findDeployment(d.Key)
	if idx < 0 {
		return 0
	}
	var total int64
	cutoff := time.Now().Add(-time.Minute)
	for _, ev := range p.deployments[idx].TokenEvents {
		if ev.At.After(cutoff) {
			total += ev.Tokens
		}
	}
	return total
}

func (p *plugin) findDeployment(key string) int {
	for i, d := range p.deployments {
		if d.Key == key {
			return i
		}
	}
	return -1
}

func parseDeployments(raw any) []deployment {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]deployment, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		params, _ := m["litellm_params"].(map[string]any)
		modelName := firstString(m["model_name"], m["model_group"], m["name"])
		provider := firstString(m["provider_instance"], m["provider"], params["provider_instance"], params["provider"])
		providerModel := firstString(m["provider_model"], m["model"], params["model"])
		if providerModel == "" {
			providerModel = modelName
		}
		if provider == "" || modelName == "" {
			continue
		}
		weight := int(intValue(firstNonNil(m["weight"], m["rpm_weight"])))
		if weight <= 0 {
			weight = 1
		}
		props := map[string]string{}
		if tags, ok := m["tags"].([]any); ok {
			parts := make([]string, 0, len(tags))
			for _, tag := range tags {
				if s := stringValue(tag); s != "" {
					parts = append(parts, s)
				}
			}
			props["tags"] = strings.Join(parts, ",")
		}
		modelInfo, _ := m["model_info"].(map[string]any)
		d := deployment{ModelName: modelName, ProviderInstance: provider, ProviderModel: providerModel, Weight: weight, RPM: intValue(m["rpm"]), TPM: intValue(m["tpm"]), Region: stringValue(m["region"]), ContextWindow: intValue(m["context_window"]), Properties: props}
		d.InputCostPer1KTokens = costPer1K(firstNonNil(m["input_cost_per_1k_tokens"], m["input_cost_per_1k"], m["cost_per_1k_input"], params["input_cost_per_1k_tokens"], params["input_cost_per_1k"], params["cost_per_1k_input"], modelInfo["input_cost_per_1k_tokens"], modelInfo["input_cost_per_1k"], modelInfo["cost_per_1k_input"]), firstNonNil(m["input_cost_per_token"], params["input_cost_per_token"], modelInfo["input_cost_per_token"]))
		d.OutputCostPer1KTokens = costPer1K(firstNonNil(m["output_cost_per_1k_tokens"], m["output_cost_per_1k"], m["cost_per_1k_output"], params["output_cost_per_1k_tokens"], params["output_cost_per_1k"], params["cost_per_1k_output"], modelInfo["output_cost_per_1k_tokens"], modelInfo["output_cost_per_1k"], modelInfo["cost_per_1k_output"]), firstNonNil(m["output_cost_per_token"], params["output_cost_per_token"], modelInfo["output_cost_per_token"]))
		d.Key = deploymentKey(d.ProviderInstance, d.ProviderModel)
		out = append(out, d)
	}
	return out
}

func parseFallbacks(raw any) map[string][]string {
	out := map[string][]string{}
	switch x := raw.(type) {
	case map[string]any:
		for k, v := range x {
			out[k] = stringSlice(v)
		}
	case []any:
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for k, v := range m {
				out[k] = stringSlice(v)
			}
		}
	}
	return out
}

func filterTimes(in []time.Time, cutoff time.Time) []time.Time {
	out := in[:0]
	for _, t := range in {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

func filterTokenEvents(in []tokenEvent, cutoff time.Time) []tokenEvent {
	out := in[:0]
	for _, ev := range in {
		if ev.At.After(cutoff) {
			out = append(out, ev)
		}
	}
	return out
}

func normalizedLatency(d deployment) float64 {
	if d.LatencyEWMA <= 0 {
		return math.MaxFloat64
	}
	return d.LatencyEWMA
}

func deploymentKey(provider, model string) string { return provider + "|" + model }
func cloneProps(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}
func costPer1K(per1K any, perToken any) float64 {
	if v := parseFloatAny(per1K); v > 0 {
		return v
	}
	if v := parseFloatAny(perToken); v > 0 {
		return v * 1000
	}
	return 0
}
func firstString(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func parseFloat(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
func parseFloatAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		return parseFloat(x)
	default:
		return 0
	}
}
func intValue(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case jsonNumber:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	default:
		return 0
	}
}

type jsonNumber string

func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		if s := stringValue(v); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := stringValue(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func main() {
	p := newPlugin()
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:    "router-litellm",
			Version: "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{
				{Type: sdk.CapabilityRouter, Name: "litellm-router", Version: "0.1.0"},
				{Type: sdk.CapabilityObserver, Name: "litellm-router-feedback", Version: "0.1.0"},
			},
		},
		Schema:     `{"type":"object","properties":{"routing_strategy":{"type":"string","enum":["simple-shuffle","weighted","least-busy","usage-based-routing","latency-based-routing","fallback","ordered"]},"strategy":{"type":"string"},"model_list":{"type":"array"},"deployments":{"type":"array"},"fallbacks":{},"context_window_fallbacks":{},"content_policy_fallbacks":{},"allowed_fails":{"type":"number"},"failure_window_seconds":{"type":"number"},"cooldown_time_seconds":{"type":"number"},"respect_rpm_tpm":{"type":"boolean"}}}`,
		Configurer: p,
		Router:     p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
