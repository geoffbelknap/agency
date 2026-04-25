package audit

// ModelPrice holds per-million-token pricing for a model alias.
type ModelPrice struct {
	InputPer1M  float64
	OutputPer1M float64
}

// EstimateCost returns USD cost for a given model and token counts.
// Returns 0 because pricing is provider/catalog-owned; callers with routing
// metadata should use EstimateCostWithPricing.
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	return EstimateCostWithPricing(nil, model, inputTokens, outputTokens)
}

// EstimateCostWithPricing returns USD cost using caller-supplied model pricing.
// Returns 0 for unknown or unpriced models.
func EstimateCostWithPricing(pricing map[string]ModelPrice, model string, inputTokens, outputTokens int) float64 {
	price, ok := pricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1_000_000)*price.InputPer1M +
		(float64(outputTokens)/1_000_000)*price.OutputPer1M
}
