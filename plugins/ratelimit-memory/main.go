package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type rule struct {
	Name              string
	Tenant            string
	Subject           string
	Model             string
	Group             string
	RequestsPerMinute int64
	TokensPerMinute   int64
}

type bucket struct {
	WindowStart time.Time
	Requests    int64
	Tokens      int64
}

type plugin struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	defaults rule
	rules    []rule
	keyBy    string
	now      func() time.Time
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buckets = map[string]*bucket{}
	p.defaults = rule{RequestsPerMinute: num(cfg["requests_per_minute"], 0), TokensPerMinute: num(cfg["tokens_per_minute"], 0)}
	p.keyBy = str(cfg["key_by"])
	if p.keyBy == "" {
		p.keyBy = "tenant_model"
	}
	p.rules = nil
	if items, _ := cfg["rules"].([]any); len(items) > 0 {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			p.rules = append(p.rules, rule{Name: str(m["name"]), Tenant: str(m["tenant"]), Subject: str(m["subject"]), Model: str(m["model"]), Group: str(m["group"]), RequestsPerMinute: num(m["requests_per_minute"], p.defaults.RequestsPerMinute), TokensPerMinute: num(m["tokens_per_minute"], p.defaults.TokensPerMinute)})
		}
	}
	return nil
}

func (p *plugin) Check(ctx context.Context, req sdk.RateLimitCheckRequest) (sdk.RateLimitCheckResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.buckets == nil {
		p.buckets = map[string]*bucket{}
	}
	now := p.clock()
	r := p.match(req)
	key := p.key(req)
	b := p.buckets[key]
	if b == nil || now.Sub(b.WindowStart) >= time.Minute {
		b = &bucket{WindowStart: now.Truncate(time.Minute)}
		p.buckets[key] = b
	}
	requestedTokens := req.EstimatedInputTokens + req.RequestedOutputTokens
	reset := b.WindowStart.Add(time.Minute)
	state := sdk.RateLimitState{RequestLimit: r.RequestsPerMinute, TokenLimit: r.TokensPerMinute, ResetUnixNano: reset.UnixNano()}
	if r.RequestsPerMinute > 0 {
		state.RequestRemaining = max(0, r.RequestsPerMinute-b.Requests)
		if b.Requests+1 > r.RequestsPerMinute {
			return deny("request rate limit exceeded", reset, state), nil
		}
	}
	if r.TokensPerMinute > 0 && requestedTokens > 0 {
		state.TokenRemaining = max(0, r.TokensPerMinute-b.Tokens)
		if b.Tokens+requestedTokens > r.TokensPerMinute {
			return deny("token rate limit exceeded", reset, state), nil
		}
	}
	b.Requests++
	b.Tokens += requestedTokens
	state.RequestRemaining = remaining(r.RequestsPerMinute, b.Requests)
	state.TokenRemaining = remaining(r.TokensPerMinute, b.Tokens)
	return sdk.RateLimitCheckResponse{Decision: "allow", State: state}, nil
}

func (p *plugin) Commit(ctx context.Context, req sdk.CommitUsageRequest) error {
	// The core calls Check before routing and Commit after success. This fixed-window
	// plugin reserves estimated tokens during Check so Commit is intentionally a no-op.
	return nil
}

func (p *plugin) match(req sdk.RateLimitCheckRequest) rule {
	for _, r := range p.rules {
		if !matches(r.Tenant, req.Identity.TenantID) || !matches(r.Subject, req.Identity.Subject) || !matches(r.Model, req.Model) {
			continue
		}
		if r.Group != "" && r.Group != "*" && !has(req.Identity.Groups, r.Group) {
			continue
		}
		return r
	}
	return p.defaults
}

func (p *plugin) key(req sdk.RateLimitCheckRequest) string {
	subject := req.Identity.Subject
	if subject == "" {
		subject = "anonymous"
	}
	tenant := req.Identity.TenantID
	if tenant == "" {
		tenant = "anonymous"
	}
	switch p.keyBy {
	case "tenant":
		return tenant
	case "subject":
		return subject
	case "subject_model":
		return subject + ":" + req.Model
	default:
		return tenant + ":" + req.Model
	}
}

func (p *plugin) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func deny(reason string, reset time.Time, state sdk.RateLimitState) sdk.RateLimitCheckResponse {
	state.ResetUnixNano = reset.UnixNano()
	return sdk.RateLimitCheckResponse{Decision: "deny", DenyReason: reason, RetryAfterMS: max(1, time.Until(reset).Milliseconds()), State: state}
}
func matches(pattern, value string) bool { return pattern == "" || pattern == "*" || pattern == value }
func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func remaining(limit, used int64) int64 {
	if limit <= 0 {
		return 0
	}
	return max(0, limit-used)
}
func max[T ~int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func num(v any, def int64) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		var n int64
		_, _ = fmt.Sscan(strings.TrimSpace(x), &n)
		if n > 0 {
			return n
		}
	}
	return def
}

func main() {
	p := &plugin{buckets: map[string]*bucket{}, keyBy: "tenant_model"}
	s := &sdk.Service{
		Metadata:    sdk.Metadata{Name: "ratelimit-memory", Version: "0.1.0", Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityRateLimiter, Name: "memory-ratelimit", Version: "0.1.0"}}, Permissions: sdk.Permissions{}},
		Schema:      `{"type":"object","properties":{"requests_per_minute":{"type":"integer"},"tokens_per_minute":{"type":"integer"},"key_by":{"type":"string","enum":["tenant","subject","tenant_model","subject_model"]},"rules":{"type":"array"}}}`,
		Configurer:  p,
		RateLimiter: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
