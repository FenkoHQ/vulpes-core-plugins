package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	baseURL         string
	apiKey          string
	organization    string
	project         string
	reasoningEffort string
	serviceTier     string
	include         []string
	http            *http.Client
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.baseURL = strings.TrimRight(stringValue(cfg["base_url"]), "/")
	p.apiKey = stringValue(cfg["api_key"])
	p.organization = stringValue(cfg["organization"])
	p.project = stringValue(cfg["project"])
	p.reasoningEffort = stringValue(cfg["reasoning_effort"])
	p.serviceTier = stringValue(cfg["service_tier"])
	p.include = stringSlice(cfg["include"])
	if p.baseURL == "" {
		p.baseURL = "https://api.openai.com/v1"
	}
	if p.apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	timeout := 180 * time.Second
	if v, ok := cfg["timeout_seconds"].(float64); ok && v > 0 {
		timeout = time.Duration(v * float64(time.Second))
	}
	p.http = &http.Client{Timeout: timeout}
	return nil
}

func (p *plugin) Invoke(ctx context.Context, req sdk.InvokeRequest) ([]sdk.ResponseChunk, error) {
	body := p.responsesRequest(req)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	baseURL := p.baseURL
	if req.Properties["base_url"] != "" {
		baseURL = strings.TrimRight(req.Properties["base_url"], "/")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(payload))
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
	if req.Request.Stream {
		return p.streamChunks(resp.Body, req), nil
	}
	var out responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_decode_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
	}
	return p.responseChunks(req, out), nil
}

func (p *plugin) ListModels(ctx context.Context) ([]sdk.ModelInfo, error) {
	if p.apiKey == "" {
		return nil, nil
	}
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
		return nil, fmt.Errorf("codex list models: %s", resp.Status)
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
		if !looksCodexModel(m.ID) {
			continue
		}
		models = append(models, sdk.ModelInfo{ID: m.ID, Object: m.Object, OwnedBy: firstString(m.OwnedBy, "openai"), ProviderInstance: "codex", ProviderModel: m.ID, SupportsStreaming: true, SupportsTools: true, SupportsJSONMode: true, Healthy: true})
	}
	return models, nil
}

func (p *plugin) responsesRequest(req sdk.InvokeRequest) map[string]any {
	body := map[string]any{
		"model": req.ProviderModel,
		"input": messagesToResponsesInput(req.Request.Messages),
	}
	if req.Request.Stream {
		body["stream"] = true
	}
	if req.Request.Temperature != nil {
		body["temperature"] = *req.Request.Temperature
	}
	if req.Request.MaxTokens != nil {
		body["max_output_tokens"] = *req.Request.MaxTokens
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
	if effort := firstString(req.Properties["reasoning_effort"], p.reasoningEffort); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	if tier := firstString(req.Properties["service_tier"], p.serviceTier); tier != "" {
		body["service_tier"] = tier
	}
	include := p.include
	if v := req.Properties["include"]; v != "" {
		include = splitCSV(v)
	}
	if len(include) > 0 {
		body["include"] = include
	}
	for k, v := range req.Request.Extra {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	return body
}

func messagesToResponsesInput(messages []sdk.ChatMessage) []map[string]any {
	input := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		input = append(input, map[string]any{
			"role":    role,
			"content": []map[string]any{{"type": contentType(role), "text": contentText(msg.Content)}},
		})
	}
	return input
}

func contentType(role string) string {
	return "input_text"
}

func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (p *plugin) streamChunks(r io.Reader, req sdk.InvokeRequest) []sdk.ResponseChunk {
	var chunks []sdk.ResponseChunk
	s := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*1024)
	s.Buffer(buf, 8*1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var ev responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Delta != "" {
			chunks = append(chunks, sdk.ResponseChunk{Chunk: &sdk.ChatCompletionChunk{ID: ev.Response.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: firstString(ev.Response.Model, req.ProviderModel), Choices: []sdk.ChatChoice{{Index: ev.OutputIndex, Delta: sdk.ChatMessage{Role: "assistant", Content: ev.Delta}}}}})
		}
		if ev.Response.Usage.TotalTokens > 0 {
			usage := usageFromResponses(req, ev.Response)
			chunks = append(chunks, sdk.ResponseChunk{Usage: &usage})
		}
	}
	if err := s.Err(); err != nil {
		chunks = append(chunks, sdk.ResponseChunk{Error: &sdk.UpstreamError{Code: "upstream_stream_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}})
	}
	return chunks
}

func (p *plugin) responseChunks(req sdk.InvokeRequest, out responsesResponse) []sdk.ResponseChunk {
	text := out.OutputText
	if text == "" {
		text = collectOutputText(out.Output)
	}
	model := firstString(out.Model, req.ProviderModel)
	id := firstString(out.ID, "resp_"+strconv.FormatInt(time.Now().UnixNano(), 36))
	chunk := sdk.ResponseChunk{Chunk: &sdk.ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: createdUnix(out.CreatedAt), Model: model, Choices: []sdk.ChatChoice{{Index: 0, Message: sdk.ChatMessage{Role: "assistant", Content: text}, FinishReason: finishReason(out.Status)}}}}
	usage := usageFromResponses(req, out)
	return []sdk.ResponseChunk{chunk, {Usage: &usage}}
}

func collectOutputText(output []responsesOutputItem) string {
	var parts []string
	for _, item := range output {
		for _, c := range item.Content {
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, "")
}

func usageFromResponses(req sdk.InvokeRequest, out responsesResponse) sdk.Usage {
	usage := sdk.Usage{ProviderInstance: req.Context.PluginInstance, ProviderModel: req.ProviderModel, InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, TotalTokens: out.Usage.TotalTokens}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	usage.CostUSD = estimateCostUSD(req.Properties, usage)
	return usage
}

func estimateCostUSD(props map[string]string, usage sdk.Usage) float64 {
	inputPer1K := firstFloatProp(props, "input_cost_per_1k_tokens", "input_cost_per_1k", "estimated_cost_per_1k_input", "cost_per_1k_input")
	outputPer1K := firstFloatProp(props, "output_cost_per_1k_tokens", "output_cost_per_1k", "estimated_cost_per_1k_output", "cost_per_1k_output")
	return float64(usage.InputTokens)/1000*inputPer1K + float64(usage.OutputTokens)/1000*outputPer1K
}

func firstFloatProp(props map[string]string, keys ...string) float64 {
	for _, key := range keys {
		if v := parseFloat(props[key]); v > 0 {
			return v
		}
	}
	return 0
}

func (p *plugin) addHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if p.organization != "" {
		req.Header.Set("OpenAI-Organization", p.organization)
	}
	if p.project != "" {
		req.Header.Set("OpenAI-Project", p.project)
	}
}

type responsesStreamEvent struct {
	Type        string            `json:"type"`
	Delta       string            `json:"delta"`
	OutputIndex int               `json:"output_index"`
	Response    responsesResponse `json:"response"`
}

type responsesResponse struct {
	ID         string                `json:"id"`
	Object     string                `json:"object"`
	CreatedAt  any                   `json:"created_at"`
	Model      string                `json:"model"`
	Status     string                `json:"status"`
	OutputText string                `json:"output_text"`
	Output     []responsesOutputItem `json:"output"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

type responsesOutputItem struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func finishReason(status string) string {
	switch status {
	case "completed", "":
		return "stop"
	case "incomplete":
		return "length"
	default:
		return status
	}
}

func createdUnix(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case string:
		if i, err := strconv.ParseInt(x, 10, 64); err == nil {
			return i
		}
	}
	return time.Now().Unix()
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

func looksCodexModel(id string) bool {
	id = strings.ToLower(id)
	return strings.Contains(id, "codex") || strings.HasPrefix(id, "gpt-5")
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

func stringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return splitCSV(x)
	default:
		return nil
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func main() {
	p := &plugin{baseURL: "https://api.openai.com/v1", http: &http.Client{Timeout: 180 * time.Second}}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "upstream-codex",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityUpstreamProvider, Name: "codex-responses", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{OutboundHosts: []string{"api.openai.com:443"}, SecretNames: []string{"CODEX_API_KEY", "OPENAI_API_KEY"}, Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}},
		},
		Schema:           `{"type":"object","required":["api_key"],"properties":{"base_url":{"type":"string"},"api_key":{"type":"string","secret":true},"organization":{"type":"string"},"project":{"type":"string"},"reasoning_effort":{"type":"string"},"service_tier":{"type":"string"},"include":{"type":["array","string"],"items":{"type":"string"}},"timeout_seconds":{"type":"number"}}}`,
		Configurer:       p,
		UpstreamProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
