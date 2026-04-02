package audit

// ModelPrice holds per-million-token pricing for a model alias.
type ModelPrice struct {
	InputPer1M  float64
	OutputPer1M float64
}

// ModelPricing maps model aliases to their pricing.
var ModelPricing = map[string]ModelPrice{
	"claude-sonnet": {InputPer1M: 3.00, OutputPer1M: 15.00},
	"claude-haiku":  {InputPer1M: 0.25, OutputPer1M: 1.25},
	"claude-opus":   {InputPer1M: 15.00, OutputPer1M: 75.00},
}

// EstimateCost returns USD cost for a given model and token counts.
// Returns 0 for unknown models (caller should log warning).
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	price, ok := ModelPricing[model]
	if !ok {
		return 0
	}
	return (float64(inputTokens)/1_000_000)*price.InputPer1M +
		(float64(outputTokens)/1_000_000)*price.OutputPer1M
}
