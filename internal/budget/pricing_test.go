package budget

import (
	"math"
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestCalculateCost(t *testing.T) {
	model := models.ModelConfig{
		CostPerMTokIn:     3.00,  // $3/MTok
		CostPerMTokOut:    15.00, // $15/MTok
		CostPerMTokCached: 0.30,  // $0.30/MTok
	}

	cost := CalculateCost(1_000_000, 100_000, 500_000, model)
	// input: 1M * $3/M = $3.00
	// output: 100K * $15/M = $1.50
	// cached: 500K * $0.30/M = $0.15
	// total: $4.65
	expected := 4.65
	if math.Abs(cost-expected) > 0.001 {
		t.Errorf("CalculateCost = %f, want %f", cost, expected)
	}
}

func TestCalculateCostZeroTokens(t *testing.T) {
	model := models.ModelConfig{
		CostPerMTokIn:  3.00,
		CostPerMTokOut: 15.00,
	}
	cost := CalculateCost(0, 0, 0, model)
	if cost != 0 {
		t.Errorf("CalculateCost with zero tokens = %f, want 0", cost)
	}
}

func TestCalculateCostNoCached(t *testing.T) {
	model := models.ModelConfig{
		CostPerMTokIn:  3.00,
		CostPerMTokOut: 15.00,
	}
	cost := CalculateCost(10_000, 5_000, 0, model)
	// input: 10K * $3/M = $0.03
	// output: 5K * $15/M = $0.075
	expected := 0.105
	if math.Abs(cost-expected) > 0.001 {
		t.Errorf("CalculateCost = %f, want %f", cost, expected)
	}
}
