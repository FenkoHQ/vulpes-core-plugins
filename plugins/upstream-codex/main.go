package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

type plugin struct {
	baseURL         string
	authMode        string
	apiKey          string
	accessToken     string
	refreshToken    string
	oauthClientID   string
	oauthTokenURL   string
	accountID       string
	backendMode     bool
	instructions    string
	organization    string
	project         string
	reasoningEffort string
	serviceTier     string
	include         []string
	http            *http.Client
	mu              sync.Mutex
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.baseURL = strings.TrimRight(stringValue(cfg["base_url"]), "/")
	p.authMode = stringValue(cfg["auth_mode"])
	p.apiKey = stringValue(cfg["api_key"])
	p.accessToken = stringValue(cfg["access_token"])
	p.refreshToken = stringValue(cfg["refresh_token"])
	p.oauthClientID = firstString(stringValue(cfg["oauth_client_id"]), "app_EMoamEEZ73f0CkXaXp7hrann")
	p.oauthTokenURL = firstString(stringValue(cfg["oauth_token_url"]), "https://auth.openai.com/oauth/token")
	p.accountID = stringValue(cfg["account_id"])
	p.backendMode = boolValue(cfg["chatgpt_backend"])
	p.instructions = firstString(stringValue(cfg["instructions"]), "You are Codex, a concise coding assistant.")
	p.organization = stringValue(cfg["organization"])
	p.project = stringValue(cfg["project"])
	p.reasoningEffort = stringValue(cfg["reasoning_effort"])
	p.serviceTier = stringValue(cfg["service_tier"])
	p.include = stringSlice(cfg["include"])
	if p.authMode == "" {
		if p.apiKey != "" {
			p.authMode = "api_key"
		} else {
			p.authMode = "oauth_env"
		}
	}
	if p.baseURL == "" {
		if p.authMode == "oauth_env" {
			p.baseURL = "https://chatgpt.com/backend-api/codex"
		} else {
			p.baseURL = "https://api.openai.com/v1"
		}
	}
	switch p.authMode {
	case "api_key":
		if p.apiKey == "" {
			return fmt.Errorf("api_key is required when auth_mode=api_key")
		}
	case "oauth_env":
		if p.accessToken == "" && p.refreshToken == "" {
			return fmt.Errorf("access_token or refresh_token is required when auth_mode=oauth_env")
		}
		if strings.Contains(p.baseURL, "chatgpt.com/backend-api/codex") {
			p.backendMode = true
		}
	default:
		return fmt.Errorf("unsupported auth_mode %q", p.authMode)
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
	do := func() (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		if err := p.addHeaders(ctx, httpReq); err != nil {
			return nil, err
		}
		return p.http.Do(httpReq)
	}
	resp, err := do()
	if err != nil {
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_request_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
	}
	defer resp.Body.Close()
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && p.canRefresh() {
		_ = resp.Body.Close()
		if err := p.refreshOAuthToken(ctx); err == nil {
			resp, err = do()
			if err != nil {
				return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_request_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
			}
			defer resp.Body.Close()
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := readLimit(resp.Body, 4096)
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: upstreamCode(resp.StatusCode), Message: msg, HTTPStatus: resp.StatusCode, Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500, RateLimited: resp.StatusCode == 429}}}, nil
	}
	if req.Request.Stream || p.backendMode {
		return p.streamChunks(resp.Body, req), nil
	}
	var out responsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return []sdk.ResponseChunk{{Error: &sdk.UpstreamError{Code: "upstream_decode_failed", Message: err.Error(), HTTPStatus: 502, Retryable: true}}}, nil
	}
	return p.responseChunks(req, out), nil
}

func (p *plugin) ListModels(ctx context.Context) ([]sdk.ModelInfo, error) {
	if p.backendMode {
		return []sdk.ModelInfo{
			{ID: "gpt-5.5", Object: "model", OwnedBy: "openai", ProviderInstance: "codex", ProviderModel: "gpt-5.5", SupportsStreaming: true, SupportsTools: true, Healthy: true},
			{ID: "gpt-5.4", Object: "model", OwnedBy: "openai", ProviderInstance: "codex", ProviderModel: "gpt-5.4", SupportsStreaming: true, SupportsTools: true, Healthy: true},
			{ID: "gpt-5.3-codex", Object: "model", OwnedBy: "openai", ProviderInstance: "codex", ProviderModel: "gpt-5.3-codex", SupportsStreaming: true, SupportsTools: true, Healthy: true},
		}, nil
	}
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
		if err != nil {
			return nil, err
		}
		if err := p.addHeaders(ctx, req); err != nil {
			return nil, err
		}
		return p.http.Do(req)
	}
	resp, err := do()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && p.canRefresh() {
		_ = resp.Body.Close()
		if err := p.refreshOAuthToken(ctx); err == nil {
			resp, err = do()
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
		}
	}
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
	if p.backendMode {
		body["instructions"] = p.instructions
		body["store"] = false
		body["stream"] = true
	} else if req.Request.Stream {
		body["stream"] = true
	}
	if req.Request.Temperature != nil {
		body["temperature"] = *req.Request.Temperature
	}
	if req.Request.MaxTokens != nil && !p.backendMode {
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

func (p *plugin) addHeaders(ctx context.Context, req *http.Request) error {
	token, err := p.bearerToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if p.organization != "" {
		req.Header.Set("OpenAI-Organization", p.organization)
	}
	if p.project != "" {
		req.Header.Set("OpenAI-Project", p.project)
	}
	if p.accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", p.accountID)
	}
	return nil
}

func (p *plugin) bearerToken(ctx context.Context) (string, error) {
	if p.authMode == "api_key" {
		return p.apiKey, nil
	}
	p.mu.Lock()
	token := p.accessToken
	refresh := p.refreshToken
	p.mu.Unlock()
	if token != "" {
		return token, nil
	}
	if refresh == "" {
		return "", fmt.Errorf("oauth access token is empty and refresh token is not configured")
	}
	if err := p.refreshOAuthToken(ctx); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.accessToken == "" {
		return "", fmt.Errorf("oauth refresh did not return an access token")
	}
	return p.accessToken, nil
}

func (p *plugin) canRefresh() bool {
	if p.authMode != "oauth_env" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshToken != ""
}

func (p *plugin) refreshOAuthToken(ctx context.Context) error {
	p.mu.Lock()
	refresh := p.refreshToken
	clientID := p.oauthClientID
	tokenURL := p.oauthTokenURL
	p.mu.Unlock()
	if refresh == "" {
		return fmt.Errorf("oauth refresh token is not configured")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("refresh oauth token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("refresh oauth token: %s: %s", resp.Status, readLimit(resp.Body, 2048))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode oauth token refresh: %w", err)
	}
	if out.AccessToken == "" {
		return fmt.Errorf("refresh oauth token: response omitted access_token")
	}
	p.mu.Lock()
	p.accessToken = out.AccessToken
	if out.RefreshToken != "" {
		p.refreshToken = out.RefreshToken
	}
	p.mu.Unlock()
	return nil
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

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(x))
		return b
	default:
		return false
	}
}

func main() {
	p := &plugin{baseURL: "https://api.openai.com/v1", http: &http.Client{Timeout: 180 * time.Second}}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "upstream-codex",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityUpstreamProvider, Name: "codex-responses", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{OutboundHosts: []string{"api.openai.com:443", "auth.openai.com:443"}, SecretNames: []string{"CODEX_API_KEY", "OPENAI_API_KEY", "CODEX_ACCESS_TOKEN", "CODEX_REFRESH_TOKEN", "CODEX_ACCOUNT_ID"}, Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}},
		},
		Schema:           `{"type":"object","properties":{"base_url":{"type":"string"},"auth_mode":{"type":"string","enum":["api_key","oauth_env"]},"api_key":{"type":"string","secret":true},"access_token":{"type":"string","secret":true},"refresh_token":{"type":"string","secret":true},"oauth_client_id":{"type":"string"},"oauth_token_url":{"type":"string"},"account_id":{"type":"string"},"chatgpt_backend":{"type":"boolean"},"instructions":{"type":"string"},"organization":{"type":"string"},"project":{"type":"string"},"reasoning_effort":{"type":"string"},"service_tier":{"type":"string"},"include":{"type":["array","string"],"items":{"type":"string"}},"timeout_seconds":{"type":"number"}}}`,
		Configurer:       p,
		UpstreamProvider: p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
