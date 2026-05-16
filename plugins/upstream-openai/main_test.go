package main

import (
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
