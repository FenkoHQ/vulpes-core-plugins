package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestMessagesToResponsesInput(t *testing.T) {
	got := messagesToResponsesInput([]sdk.ChatMessage{{Role: "user", Content: "hello"}})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0]["role"] != "user" {
		t.Fatalf("role = %v", got[0]["role"])
	}
	content, ok := got[0]["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", got[0]["content"])
	}
	if content[0]["type"] != "input_text" || content[0]["text"] != "hello" {
		t.Fatalf("content[0] = %#v", content[0])
	}
}

func TestCollectOutputText(t *testing.T) {
	out := []responsesOutputItem{{Content: []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "output_text", Text: "hello"}, {Type: "output_text", Text: " world"}}}}
	if got := collectOutputText(out); got != "hello world" {
		t.Fatalf("text = %q", got)
	}
}

func TestEstimateCostUSD(t *testing.T) {
	usage := sdk.Usage{InputTokens: 1000, OutputTokens: 500}
	got := estimateCostUSD(map[string]string{"input_cost_per_1k_tokens": "0.001", "output_cost_per_1k_tokens": "0.002"}, usage)
	if got != 0.002 {
		t.Fatalf("cost = %v, want 0.002", got)
	}
}

func TestConfigureOAuthEnvWithoutAPIKey(t *testing.T) {
	p := &plugin{}
	err := p.Configure(context.Background(), map[string]any{"auth_mode": "oauth_env", "access_token": "access"}, nil)
	if err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if p.authMode != "oauth_env" {
		t.Fatalf("authMode = %q, want oauth_env", p.authMode)
	}
}

func TestOAuthRefreshAndRetry(t *testing.T) {
	var gotRefresh bool
	var responsesCalls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			gotRefresh = true
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" {
				t.Fatalf("bad refresh form: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "new-access", "refresh_token": "new-refresh"})
		case "/responses":
			responsesCalls++
			if responsesCalls == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Fatalf("Authorization = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "resp_1",
				"model":       "gpt-5-codex",
				"status":      "completed",
				"output_text": "ok",
				"usage": map[string]int{
					"input_tokens":  1,
					"output_tokens": 1,
					"total_tokens":  2,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := &plugin{}
	if err := p.Configure(context.Background(), map[string]any{
		"base_url":        ts.URL,
		"auth_mode":       "oauth_env",
		"access_token":    "expired-access",
		"refresh_token":   "refresh",
		"oauth_token_url": ts.URL + "/oauth/token",
	}, nil); err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	chunks, err := p.Invoke(context.Background(), sdk.InvokeRequest{ProviderModel: "gpt-5-codex", Request: sdk.ChatCompletionRequest{Messages: []sdk.ChatMessage{{Role: "user", Content: "hi"}}}})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if !gotRefresh {
		t.Fatal("refresh endpoint was not called")
	}
	if responsesCalls != 2 {
		t.Fatalf("responses calls = %d, want 2", responsesCalls)
	}
	if len(chunks) == 0 || chunks[0].Chunk == nil || chunks[0].Chunk.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}
