package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	baseURL      string
	apiKey       string
	organization string
	http         *http.Client
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.baseURL = strings.TrimRight(stringValue(cfg["base_url"]), "/")
	p.apiKey = stringValue(cfg["api_key"])
	p.organization = stringValue(cfg["organization"])
	if p.baseURL == "" {
		p.baseURL = "https://api.openai.com/v1"
	}
	if p.apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	timeout := 120 * time.Second
	if v, ok := cfg["timeout_seconds"].(float64); ok && v > 0 {
		timeout = time.Duration(v * float64(time.Second))
	}
	p.http = &http.Client{Timeout: timeout}
	return nil
}

func (p *plugin) Invoke(ctx context.Context, req sdk.InvokeRequest) ([]sdk.ResponseChunk, error) {
	body := p.openAIRequest(req)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	p.addHeaders(httpReq)
	resp, err := p.http.Do(httpReq)
	if err != nil {
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_request_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := readLimit(resp.Body, 4096)
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: upstreamCode(resp.StatusCode), Message: msg, HTTPStatus: resp.StatusCode, Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500, RateLimited: resp.StatusCode == 429}}}, nil
	}
	var out openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_decode_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
	}
	return p.responseChunks(req, out), nil
}

func (p *plugin) ListModels(ctx context.Context) ([]sdk.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.addHeaders(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("openai list models: %s", resp.Status)
	}
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	models := make([]sdk.ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		models = append(models, sdk.ModelInfo{ID: m.ID, Object: m.Object, OwnedBy: m.OwnedBy, ProviderInstance: "openai", ProviderModel: m.ID, SupportsStreaming: true, SupportsTools: true, SupportsJSONMode: true, Healthy: true})
	}
	return models, nil
}

func (p *plugin) openAIRequest(req sdk.InvokeRequest) map[string]any {
	body := map[string]any{
		"model":    req.ProviderModel,
		"messages": req.Request.Messages,
		"stream":   false,
	}
	if req.Request.Temperature != nil {
		body["temperature"] = *req.Request.Temperature
	}
	if req.Request.MaxTokens != nil {
		body["max_tokens"] = *req.Request.MaxTokens
	}
	if req.Request.Tools != nil {
		body["tools"] = req.Request.Tools
	}
	if req.Request.ToolChoice != nil {
		body["tool_choice"] = req.Request.ToolChoice
	}
	if req.Request.Metadata != nil {
		body["metadata"] = req.Request.Metadata
	}
	for k, v := range req.Request.Extra {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	return body
}

func (p *plugin) responseChunks(req sdk.InvokeRequest, out openAIChatResponse) []sdk.ResponseChunk {
	chunks := make([]sdk.ResponseChunk, 0, len(out.Choices)+1)
	for _, choice := range out.Choices {
		content := choice.Message.Content
		msg := sdk.ChatMessage{Role: choice.Message.Role, Content: content}
		ch := sdk.ChatChoice{Index: choice.Index, FinishReason: choice.FinishReason}
		if req.Request.Stream {
			ch.Delta = msg
		} else {
			ch.Message = msg
		}
		chunks = append(chunks, sdk.ResponseChunk{Chunk: &sdk.ChatCompletionChunk{ID: out.ID, Object: "chat.completion.chunk", Created: out.Created, Model: out.Model, Choices: []sdk.ChatChoice{ch}}})
	}
	chunks = append(chunks, sdk.ResponseChunk{Usage: &sdk.Usage{ProviderInstance: req.Context.PluginInstance, ProviderModel: req.ProviderModel, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens}})
	return chunks
}

func (p *plugin) addHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if p.organization != "" {
		req.Header.Set("OpenAI-Organization", p.organization)
	}
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

func upstreamCode(status int) string {
	switch {
	case status == 429:
		return "upstream_rate_limited"
	case status >= 500:
		return "upstream_5xx"
	case status == 401 || status == 403:
		return "auth_error"
	default:
		return "invalid_request"
	}
}

func readLimit(r io.Reader, n int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, n))
	return string(b)
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func main() {
	p := &plugin{baseURL: "https://api.openai.com/v1", http: &http.Client{Timeout: 120 * time.Second}}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "upstream-openai",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityUpstreamProvider, Name: "openai-chat-completions", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{OutboundHosts: []string{"api.openai.com:443"}, SecretNames: []string{"OPENAI_API_KEY"}, Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}},
		},
		Schema:           `{"type":"object","required":["api_key"],"properties":{"base_url":{"type":"string"},"api_key":{"type":"string"},"organization":{"type":"string"},"timeout_seconds":{"type":"number"}}}`,
		Configurer:       p,
		UpstreamProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
