package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type entry struct {
	Response  sdk.CachedResponse
	ExpiresAt time.Time
	Size      int
	LastUsed  time.Time
}

type plugin struct {
	mu           sync.Mutex
	items        map[string]entry
	defaultTTL   time.Duration
	maxEntries   int
	namespace    string
	includeModel bool
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = map[string]entry{}
	p.defaultTTL = durationMS(num(cfg["default_ttl_ms"], 60000))
	p.maxEntries = int(num(cfg["max_entries"], 10000))
	p.namespace = str(cfg["namespace"])
	p.includeModel = true
	if v, ok := cfg["include_model"].(bool); ok {
		p.includeModel = v
	}
	if p.maxEntries <= 0 {
		return fmt.Errorf("max_entries must be positive")
	}
	return nil
}

func (p *plugin) Lookup(ctx context.Context, req sdk.CacheLookupRequest) (sdk.CacheLookupResponse, error) {
	key := p.key(req.Identity, req.CacheKey, "")
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		p.items = map[string]entry{}
	}
	it, ok := p.items[key]
	if !ok {
		return sdk.CacheLookupResponse{Hit: false}, nil
	}
	if !it.ExpiresAt.IsZero() && now.After(it.ExpiresAt) {
		delete(p.items, key)
		return sdk.CacheLookupResponse{Hit: false}, nil
	}
	it.LastUsed = now
	p.items[key] = it
	return sdk.CacheLookupResponse{Hit: true, Response: it.Response, CacheID: cacheID(key)}, nil
}

func (p *plugin) Store(ctx context.Context, req sdk.CacheStoreRequest) error {
	ttl := p.defaultTTL
	if req.Policy.TTLMillis > 0 {
		ttl = durationMS(req.Policy.TTLMillis)
	}
	if ttl <= 0 {
		return nil
	}
	key := p.key(req.Identity, req.CacheKey, "")
	b, _ := json.Marshal(req.Response)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		p.items = map[string]entry{}
	}
	p.items[key] = entry{Response: req.Response, ExpiresAt: time.Now().Add(ttl), Size: len(b), LastUsed: time.Now()}
	p.evictLocked()
	return nil
}

func (p *plugin) key(id sdk.Identity, cacheKey, model string) string {
	k := p.namespace + ":" + id.TenantID + ":" + cacheKey
	return k
}

func (p *plugin) evictLocked() {
	for len(p.items) > p.maxEntries {
		oldestKey := ""
		var oldest time.Time
		for k, v := range p.items {
			if oldestKey == "" || v.LastUsed.Before(oldest) {
				oldestKey = k
				oldest = v.LastUsed
			}
		}
		delete(p.items, oldestKey)
	}
}

func cacheID(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:8])
}
func durationMS(v int64) time.Duration { return time.Duration(v) * time.Millisecond }
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
	default:
		return def
	}
}

func main() {
	p := &plugin{items: map[string]entry{}, defaultTTL: time.Minute, maxEntries: 10000, includeModel: true}
	s := &sdk.Service{
		Metadata:      sdk.Metadata{Name: "cache-memory", Version: "0.1.0", Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityCacheProvider, Name: "memory-cache", Version: "0.1.0"}}, Permissions: sdk.Permissions{Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}}},
		Schema:        `{"type":"object","properties":{"default_ttl_ms":{"type":"integer","default":60000},"max_entries":{"type":"integer","default":10000},"namespace":{"type":"string"},"include_model":{"type":"boolean","default":true}}}`,
		Configurer:    p,
		CacheProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
