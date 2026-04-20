package state

import "strings"

// ModelPricing records input/output token pricing per million tokens (USD).
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// modelPricing is the authoritative pricing table. Update when providers
// change their rates. Source: https://www.anthropic.com/pricing (checked
// 2026-04 for Claude; z.ai pricing mirrored from their docs at the same time).
// Keys are lowercased match prefixes -- the lookup does a prefix match so
// variants like "claude-opus-4-7-2026..." resolve correctly.
var modelPricing = map[string]ModelPricing{
	// Claude family (USD per million tokens)
	"claude-opus":   {InputPerMillion: 15.00, OutputPerMillion: 75.00},
	"claude-sonnet": {InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-haiku":  {InputPerMillion: 0.80, OutputPerMillion: 4.00},
	// Legacy aliases occasionally emitted by the SDK
	"claude-3-5-sonnet": {InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-3-opus":     {InputPerMillion: 15.00, OutputPerMillion: 75.00},
	// Z.AI GLM rates (as of 2026-04, approximate USD equivalents)
	"glm-5":   {InputPerMillion: 0.60, OutputPerMillion: 2.20},
	"glm-4.7": {InputPerMillion: 0.50, OutputPerMillion: 1.50},
	"glm-4.5": {InputPerMillion: 0.10, OutputPerMillion: 0.30},
}

// defaultPricing is used when the model string is empty or unknown so we still
// record a plausible cost instead of dropping the entry silently. Mirrors
// Sonnet rates -- the most common default on the platform.
var defaultPricing = ModelPricing{InputPerMillion: 3.00, OutputPerMillion: 15.00}

// ComputeCost returns the USD cost for the given token counts and model.
// Unknown models fall back to Sonnet-equivalent pricing.
func ComputeCost(model string, inputTokens, outputTokens int64) float64 {
	p := LookupPricing(model)
	return (float64(inputTokens)/1_000_000)*p.InputPerMillion +
		(float64(outputTokens)/1_000_000)*p.OutputPerMillion
}

// LookupPricing returns the pricing entry for a model, falling back to
// defaultPricing (Sonnet rates) for empty or unrecognized strings.
func LookupPricing(model string) ModelPricing {
	if model == "" {
		return defaultPricing
	}
	m := strings.ToLower(model)
	// Exact match first.
	if p, ok := modelPricing[m]; ok {
		return p
	}
	// Prefix match -- handles variants like "claude-sonnet-4-6-20251001".
	for prefix, p := range modelPricing {
		if strings.HasPrefix(m, prefix) {
			return p
		}
	}
	return defaultPricing
}
