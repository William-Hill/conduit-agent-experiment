package cost

import (
	"log"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Price holds per-million-token pricing for a model.
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

var modelPrices = map[string]Price{
	"gemini-2.5-flash":           {InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"claude-haiku-4-5-20251001":  {InputPerMTok: 1.00, OutputPerMTok: 5.00},
	"claude-sonnet-4-6-20250514": {InputPerMTok: 3.00, OutputPerMTok: 15.00},
}

func Calculate(model string, inputTokens, outputTokens int) float64 {
	price, ok := modelPrices[model]
	if !ok {
		log.Printf("cost: unknown model %q, returning $0", model)
		return 0.0
	}
	inputCost := float64(inputTokens) / 1_000_000 * price.InputPerMTok
	outputCost := float64(outputTokens) / 1_000_000 * price.OutputPerMTok
	return inputCost + outputCost
}

func CalculateCalls(calls []models.LLMCall) float64 {
	var total float64
	for _, c := range calls {
		total += Calculate(c.Model, c.InputTokens, c.OutputTokens)
	}
	return total
}
