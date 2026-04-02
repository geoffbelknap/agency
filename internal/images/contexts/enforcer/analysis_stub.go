package main

// AnalysisClient is a stub for the removed analysis service client.
// The analysis service was deleted; all call sites in llm.go guard against
// nil so this type only needs to satisfy the compiler.
type AnalysisClient struct{}

// UsageData carries token usage and cost metrics for analysis reporting.
type UsageData struct {
	Agent        string
	InputTokens  int
	OutputTokens int
	CostIn       float64
	CostOut      float64
	Model        string
	StatusCode   int
	LatencyMs    int64
}

func (a *AnalysisClient) CheckBudget(agent string) (allowed bool, reason string, err error) {
	return true, "", nil
}

func (a *AnalysisClient) AcquireRateLimit(provider, agent string) (granted bool, waitSecs float64, err error) {
	return true, 0, nil
}

func (a *AnalysisClient) UpdateRateLimit(provider string, limitReqs, remainingReqs int, resetSecs float64, statusCode int, retryAfter float64) {
}

func (a *AnalysisClient) PostUsage(u UsageData) {}

func (a *AnalysisClient) PostScan(agent, content, contentType string) {}
