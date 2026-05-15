package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	mu                 sync.Mutex
	listen             string
	path               string
	namespace          string
	enableTenantLabels bool
	serverStarted      bool

	events         map[labelKey]float64
	usageInput     map[usageKey]float64
	usageOutput    map[usageKey]float64
	usageTotal     map[usageKey]float64
	usageCostUSD   map[usageKey]float64
	requestResults map[resultKey]float64
}

type labelKey struct {
	EventType string
	TenantID  string
}

type usageKey struct {
	Provider string
	Model    string
	TenantID string
}

type resultKey struct {
	Status   string
	TenantID string
}

func newPlugin() *plugin {
	return &plugin{
		listen:         "127.0.0.1:9090",
		path:           "/metrics",
		namespace:      "vulpes_gateway",
		events:         map[labelKey]float64{},
		usageInput:     map[usageKey]float64{},
		usageOutput:    map[usageKey]float64{},
		usageTotal:     map[usageKey]float64{},
		usageCostUSD:   map[usageKey]float64{},
		requestResults: map[resultKey]float64{},
	}
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	if s := stringValue(cfg["listen"]); s != "" {
		p.listen = s
	}
	if s := stringValue(cfg["path"]); s != "" {
		p.path = s
	}
	if !strings.HasPrefix(p.path, "/") {
		p.path = "/" + p.path
	}
	if s := stringValue(cfg["namespace"]); s != "" {
		p.namespace = sanitizeMetricName(s)
	}
	if v, ok := cfg["enable_tenant_labels"].(bool); ok {
		p.enableTenantLabels = v
	}
	start := !p.serverStarted
	if start {
		p.serverStarted = true
	}
	listen := p.listen
	path := p.path
	p.mu.Unlock()

	if start {
		mux := http.NewServeMux()
		mux.HandleFunc(path, p.handleMetrics)
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
		go func() {
			log.Printf("prometheus observer listening on %s%s", listen, path)
			if err := http.ListenAndServe(listen, mux); err != nil && err != http.ErrServerClosed {
				log.Printf("prometheus observer stopped: %v", err)
			}
		}()
	}
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ev := range events {
		tenant := ""
		if p.enableTenantLabels {
			tenant = ev.TenantID
		}
		p.events[labelKey{EventType: ev.EventType, TenantID: tenant}]++
		if ev.EventType == "request.completed" {
			p.requestResults[resultKey{Status: "completed", TenantID: tenant}]++
		}
		if ev.EventType == "request.failed" {
			p.requestResults[resultKey{Status: "failed", TenantID: tenant}]++
		}
		if ev.Usage.InputTokens != 0 || ev.Usage.OutputTokens != 0 || ev.Usage.TotalTokens != 0 || ev.Usage.CostUSD != 0 {
			key := usageKey{Provider: ev.Usage.ProviderInstance, Model: ev.Usage.ProviderModel, TenantID: tenant}
			p.usageInput[key] += float64(ev.Usage.InputTokens)
			p.usageOutput[key] += float64(ev.Usage.OutputTokens)
			p.usageTotal[key] += float64(ev.Usage.TotalTokens)
			p.usageCostUSD[key] += ev.Usage.CostUSD
		}
	}
	return nil
}

func (p *plugin) handleMetrics(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	ns := p.namespace

	writeHelp(w, ns+"_events_total", "Gateway lifecycle events observed by this plugin.", "counter")
	for _, key := range sortedLabelKeys(p.events) {
		labels := map[string]string{"event_type": key.EventType}
		if p.enableTenantLabels {
			labels["tenant_id"] = key.TenantID
		}
		writeSample(w, ns+"_events_total", labels, p.events[key])
	}

	writeHelp(w, ns+"_requests_total", "Gateway request results observed by this plugin.", "counter")
	for _, key := range sortedResultKeys(p.requestResults) {
		labels := map[string]string{"status": key.Status}
		if p.enableTenantLabels {
			labels["tenant_id"] = key.TenantID
		}
		writeSample(w, ns+"_requests_total", labels, p.requestResults[key])
	}

	writeUsageMap(w, ns+"_usage_input_tokens_total", "Input tokens observed by this plugin.", p.usageInput, p.enableTenantLabels)
	writeUsageMap(w, ns+"_usage_output_tokens_total", "Output tokens observed by this plugin.", p.usageOutput, p.enableTenantLabels)
	writeUsageMap(w, ns+"_usage_total_tokens_total", "Total tokens observed by this plugin.", p.usageTotal, p.enableTenantLabels)
	writeUsageMap(w, ns+"_usage_cost_usd_total", "Estimated/actual cost in USD observed by this plugin.", p.usageCostUSD, p.enableTenantLabels)
}

func writeUsageMap(w http.ResponseWriter, name, help string, values map[usageKey]float64, tenant bool) {
	writeHelp(w, name, help, "counter")
	for _, key := range sortedUsageKeys(values) {
		labels := map[string]string{"provider": key.Provider, "model": key.Model}
		if tenant {
			labels["tenant_id"] = key.TenantID
		}
		writeSample(w, name, labels, values[key])
	}
}

func writeHelp(w http.ResponseWriter, name, help, typ string) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", name, typ)
}

func writeSample(w http.ResponseWriter, name string, labels map[string]string, value float64) {
	_, _ = fmt.Fprintf(w, "%s%s %s\n", name, formatLabels(labels), strconv.FormatFloat(value, 'f', -1, 64))
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, sanitizeLabelName(k), escapeLabelValue(labels[k])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sortedLabelKeys(m map[labelKey]float64) []labelKey {
	keys := make([]labelKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].EventType == keys[j].EventType {
			return keys[i].TenantID < keys[j].TenantID
		}
		return keys[i].EventType < keys[j].EventType
	})
	return keys
}

func sortedUsageKeys(m map[usageKey]float64) []usageKey {
	keys := make([]usageKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Provider != keys[j].Provider {
			return keys[i].Provider < keys[j].Provider
		}
		if keys[i].Model != keys[j].Model {
			return keys[i].Model < keys[j].Model
		}
		return keys[i].TenantID < keys[j].TenantID
	})
	return keys
}

func sortedResultKeys(m map[resultKey]float64) []resultKey {
	keys := make([]resultKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Status == keys[j].Status {
			return keys[i].TenantID < keys[j].TenantID
		}
		return keys[i].Status < keys[j].Status
	})
	return keys
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func sanitizeMetricName(s string) string {
	if s == "" {
		return "vulpes_gateway"
	}
	var b strings.Builder
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func sanitizeLabelName(s string) string { return sanitizeMetricName(s) }

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func main() {
	p := newPlugin()
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-prometheus",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "prometheus", Version: "0.1.0"}},
		},
		Schema:     `{"type":"object","properties":{"listen":{"type":"string"},"path":{"type":"string"},"namespace":{"type":"string"},"enable_tenant_labels":{"type":"boolean"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
