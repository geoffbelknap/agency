package budget

import "github.com/geoffbelknap/agency/internal/models"

// CalculateCost computes USD cost from token counts and model pricing.
// Token costs in ModelConfig are per million tokens.
func CalculateCost(inputTokens, outputTokens, cachedTokens int64, model models.ModelConfig) float64 {
	inputCost := float64(inputTokens) * model.CostPerMTokIn / 1_000_000
	outputCost := float64(outputTokens) * model.CostPerMTokOut / 1_000_000
	cachedCost := float64(cachedTokens) * model.CostPerMTokCached / 1_000_000
	return inputCost + outputCost + cachedCost
}
