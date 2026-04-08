package cost

import (
	"log"
	"sync"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// warnedModels tracks which unknown models have already been warned about.
var warnedModels sync.Map

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

// fallbackPrice is the most expensive known model's pricing, used for unknown
// models so that budget checks fail safe by over-counting rather than under-counting.
var fallbackPrice = Price{InputPerMTok: 3.00, OutputPerMTok: 15.00}

func priceFor(model string) Price {
	price, ok := modelPrices[model]
	if !ok {
		if _, seen := warnedModels.LoadOrStore(model, true); !seen {
			log.Printf("cost: unknown model %q, using fallback pricing", model)
		}
		return fallbackPrice
	}
	return price
}

// Calculate returns the cost in USD for the given token counts and model.
// Unknown models use the most expensive known pricing as a safe fallback.
func Calculate(model string, inputTokens, outputTokens int) float64 {
	price := priceFor(model)
	inputCost := float64(inputTokens) / 1_000_000 * price.InputPerMTok
	outputCost := float64(outputTokens) / 1_000_000 * price.OutputPerMTok
	return inputCost + outputCost
}

// CalculateWithCache returns the cost including Anthropic prompt-cache tokens.
// Cache creation is billed at 1.25x input price, cache read at 0.1x.
func CalculateWithCache(model string, inputTokens, cacheCreateTokens, cacheReadTokens, outputTokens int) float64 {
	price := priceFor(model)
	inputCost := float64(inputTokens) / 1_000_000 * price.InputPerMTok
	cacheCreateCost := float64(cacheCreateTokens) / 1_000_000 * price.InputPerMTok * 1.25
	cacheReadCost := float64(cacheReadTokens) / 1_000_000 * price.InputPerMTok * 0.10
	outputCost := float64(outputTokens) / 1_000_000 * price.OutputPerMTok
	return inputCost + cacheCreateCost + cacheReadCost + outputCost
}

// CalculateCalls sums cost across a slice of LLM calls, using cache-aware
// pricing when cache token fields are populated.
func CalculateCalls(calls []models.LLMCall) float64 {
	var total float64
	for _, c := range calls {
		if c.CacheCreationTokens > 0 || c.CacheReadTokens > 0 {
			total += CalculateWithCache(c.Model, c.InputTokens, c.CacheCreationTokens, c.CacheReadTokens, c.OutputTokens)
		} else {
			total += Calculate(c.Model, c.InputTokens, c.OutputTokens)
		}
	}
	return total
}
