package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageAddAndCost(t *testing.T) {
	var u Usage
	u.Add(Usage{Calls: 1, InputTokens: 1_000_000, OutputTokens: 100_000})
	u.Add(Usage{Calls: 1, CacheReadTokens: 2_000_000, CacheWriteTokens: 1_000_000})
	require.Equal(t, 2, u.Calls)
	// 5.00 + 2.5 + 1.0 + 6.25
	require.InDelta(t, 14.75, u.Cost(DefaultPricing), 1e-9)
}

func TestPricingFromMap(t *testing.T) {
	p := PricingFromMap(map[string]float64{"input": 1, "output": 2})
	require.Equal(t, 1.0, p.Input)
	require.Equal(t, 2.0, p.Output)
	require.Equal(t, DefaultPricing.CacheRead, p.CacheRead)
	require.Equal(t, DefaultPricing, PricingFromMap(nil))
}
