package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
)

func TestEstimateCostUSD(t *testing.T) {
	usage := sdk.Usage{InputTokens: 1000, OutputTokens: 500}
	got := estimateCostUSD(map[string]string{"input_cost_per_1k_tokens": "0.001", "output_cost_per_1k_tokens": "0.002"}, usage)
	if got != 0.002 {
		t.Fatalf("cost = %v, want 0.002", got)
	}
}

// aliasToolCallResponse is a real captured response from alias1 when given a
// tools payload. Before the tool_calls plumbing landed, the gateway parsed
// only role+content, dropped tool_calls silently, and fell back to surfacing
// reasoning_content as the assistant message — which is what pi rendered as
// "I'll use the bash command to execute 'ls'" without ever invoking a tool.
const aliasToolCallResponse = `{
  "id":"0d9e11d5",
  "created":1779867263,
  "model":"alias1",
  "object":"chat.completion",
  "choices":[{
    "finish_reason":"tool_calls",
    "index":0,
    "message":{
      "role":"assistant",
      "tool_calls":[{
        "index":0,
        "function":{"arguments":"{\"cmd\": \"ls -la\"}","name":"bash"},
        "id":"call_54fe2f53",
        "type":"function"
      }],
      "reasoning_content":"The user wants to list files in the current directory.",
      "content":null
    }
  }],
  "usage":{"completion_tokens":77,"prompt_tokens":276,"total_tokens":353}
}`

func TestResponseChunks_PreservesToolCalls(t *testing.T) {
	var resp openAIChatResponse
	if err := json.NewDecoder(bytes.NewReader([]byte(aliasToolCallResponse))).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p := &plugin{}
	out := make(chan sdk.ResponseChunk, 8)
	p.responseChunks(sdk.InvokeRequest{ProviderModel: "alias1"}, resp, out)
	close(out)
	var chunks []sdk.ResponseChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	if len(chunks) == 0 || chunks[0].Chunk == nil {
		t.Fatalf("expected at least one chunk")
	}
	msg := chunks[0].Chunk.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %v, want 1", msg.ToolCalls)
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_54fe2f53" || tc.Type != "function" {
		t.Errorf("tool call id/type = %q/%q", tc.ID, tc.Type)
	}
	if tc.Function.Name != "bash" || !strings.Contains(tc.Function.Arguments, `"cmd"`) {
		t.Errorf("tool call function = %+v", tc.Function)
	}
	if chunks[0].Chunk.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", chunks[0].Chunk.Choices[0].FinishReason)
	}
}

// Streaming: an SSE delta carrying tool_calls must surface on the chunk's
// Delta.ToolCalls — and reasoning_content must not leak into Content when a
// tool call is in flight (the old fallback would shadow the structured call).
const aliasStreamWithToolCall = "data: {\"id\":\"x\",\"created\":1,\"model\":\"alias1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"thinking…\",\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\ndata: [DONE]\n"

func TestStreamChunks_PreservesToolCalls(t *testing.T) {
	p := &plugin{}
	out := make(chan sdk.ResponseChunk, 8)
	if err := p.streamChunks(context.Background(), strings.NewReader(aliasStreamWithToolCall), sdk.InvokeRequest{ProviderModel: "alias1"}, out); err != nil {
		t.Fatalf("streamChunks: %v", err)
	}
	close(out)
	var chunks []sdk.ResponseChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	if len(chunks) == 0 || chunks[0].Chunk == nil {
		t.Fatalf("expected chunk")
	}
	delta := chunks[0].Chunk.Choices[0].Delta
	if len(delta.ToolCalls) != 1 || delta.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("tool_calls not surfaced: %+v", delta.ToolCalls)
	}
	if delta.Content != "" {
		t.Errorf("content leaked: %q (expected empty so reasoning doesn't shadow tool call)", delta.Content)
	}
}
