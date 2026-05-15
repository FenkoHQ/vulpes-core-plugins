package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	address                string
	token                  string
	service                string
	scheme                 string
	providerInstance       string
	tagPrefixModel         string
	tagPrefixProviderModel string
	tagPrefixBaseURL       string
	strategy               string
	http                   *http.Client
}

type consulEntry struct {
	Service struct {
		Address string            `json:"Address"`
		Port    int               `json:"Port"`
		Tags    []string          `json:"Tags"`
		Service string            `json:"Service"`
		ID      string            `json:"ID"`
		Meta    map[string]string `json:"Meta"`
	} `json:"Service"`
	Node struct {
		Address string `json:"Address"`
	} `json:"Node"`
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.address = firstString(cfg["address"], "http://127.0.0.1:8500")
	p.token = firstString(cfg["token"], secrets["CONSUL_HTTP_TOKEN"])
	p.service = firstString(cfg["service"], "litellm")
	p.scheme = firstString(cfg["scheme"], "http")
	p.providerInstance = firstString(cfg["provider_instance"], "litellm")
	p.tagPrefixModel = firstString(cfg["tag_prefix_model"], "model:")
	p.tagPrefixProviderModel = firstString(cfg["tag_prefix_provider_model"], "provider_model:")
	p.tagPrefixBaseURL = firstString(cfg["tag_prefix_base_url"], "base_url:")
	p.strategy = firstString(cfg["strategy"], "shuffle")
	p.http = &http.Client{Timeout: 5 * time.Second}
	return nil
}

func (p *plugin) Route(ctx context.Context, req sdk.RouteRequest) (sdk.RouteResponse, error) {
	entries, err := p.discover(ctx)
	if err != nil {
		return sdk.RouteResponse{}, err
	}
	var routes []sdk.SelectedRoute
	for _, e := range entries {
		logical := firstTag(e.Service.Tags, p.tagPrefixModel)
		if logical == "" {
			logical = e.Service.Meta["model"]
		}
		if logical != req.RequestedModel {
			continue
		}
		providerModel := firstTag(e.Service.Tags, p.tagPrefixProviderModel)
		if providerModel == "" {
			providerModel = e.Service.Meta["provider_model"]
		}
		if providerModel == "" {
			providerModel = req.RequestedModel
		}
		baseURL := firstTag(e.Service.Tags, p.tagPrefixBaseURL)
		if baseURL == "" {
			baseURL = e.Service.Meta["base_url"]
		}
		if baseURL == "" {
			baseURL = fmt.Sprintf("%s://%s:%d/v1", p.scheme, serviceAddress(e), e.Service.Port)
		}
		props := map[string]string{"base_url": strings.TrimRight(baseURL, "/"), "consul_service": e.Service.Service, "consul_service_id": e.Service.ID}
		routes = append(routes, sdk.SelectedRoute{ProviderInstance: p.providerInstance, ProviderModel: providerModel, Priority: len(routes), Properties: props})
	}
	if p.strategy == "shuffle" {
		rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })
	} else {
		sort.SliceStable(routes, func(i, j int) bool {
			return routes[i].Properties["consul_service_id"] < routes[j].Properties["consul_service_id"]
		})
	}
	for i := range routes {
		routes[i].Priority = i
	}
	return sdk.RouteResponse{Routes: routes, Reason: "consul_" + p.strategy}, nil
}

func (p *plugin) discover(ctx context.Context) ([]consulEntry, error) {
	url := strings.TrimRight(p.address, "/") + "/v1/health/service/" + p.service + "?passing=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("X-Consul-Token", p.token)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("consul status %s", resp.Status)
	}
	var entries []consulEntry
	return entries, json.NewDecoder(resp.Body).Decode(&entries)
}

func serviceAddress(e consulEntry) string {
	if e.Service.Address != "" {
		return e.Service.Address
	}
	return e.Node.Address
}
func firstTag(tags []string, prefix string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			return strings.TrimPrefix(tag, prefix)
		}
	}
	return ""
}
func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func main() {
	p := &plugin{}
	s := &sdk.Service{Metadata: sdk.Metadata{Name: "router-consul", Version: "0.1.0", Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityRouter, Name: "consul-router", Version: "0.1.0"}}, Permissions: sdk.Permissions{SecretNames: []string{"CONSUL_HTTP_TOKEN"}, Data: sdk.DataPermissions{ReadPrompt: false}}}, Schema: `{"type":"object","properties":{"address":{"type":"string"},"token":{"type":"string"},"service":{"type":"string"},"scheme":{"type":"string"},"provider_instance":{"type":"string"},"strategy":{"type":"string"}}}`, Configurer: p, Router: p}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
