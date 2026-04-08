package cost

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCalculateGeminiFlash(t *testing.T) {
	got := Calculate("gemini-2.5-flash", 1000, 500)
	want := 0.00045
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Calculate(gemini-2.5-flash, 1000, 500) = %f, want %f", got, want)
	}
}

func TestCalculateHaiku(t *testing.T) {
	got := Calculate("claude-haiku-4-5-20251001", 1000, 500)
	want := 0.0035
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Calculate(haiku, 1000, 500) = %f, want %f", got, want)
	}
}

func TestCalculateUnknownModelUsesFallback(t *testing.T) {
	// Unknown models use Sonnet pricing (most expensive) as a safe fallback.
	got := Calculate("unknown-model", 1000, 500)
	want := Calculate("claude-sonnet-4-6-20250514", 1000, 500)
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Calculate(unknown, 1000, 500) = %f, want fallback %f", got, want)
	}
}

func TestCalculateWithCache(t *testing.T) {
	// Haiku: $1.00/MTok input, $5.00/MTok output
	// 100 base input at 1.0x = $0.0001
	// 5000 cache-create at 1.25x = $0.00625
	// 10000 cache-read at 0.1x = $0.001
	// 500 output = $0.0025
	got := CalculateWithCache("claude-haiku-4-5-20251001", 100, 5000, 10000, 500)
	want := 0.0001 + 0.00625 + 0.001 + 0.0025
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CalculateWithCache() = %f, want %f", got, want)
	}
}

func TestCalculateCallsWithCacheTokens(t *testing.T) {
	calls := []models.LLMCall{
		{Model: "claude-haiku-4-5-20251001", InputTokens: 100, OutputTokens: 500, CacheCreationTokens: 5000, CacheReadTokens: 10000},
	}
	got := CalculateCalls(calls)
	want := CalculateWithCache("claude-haiku-4-5-20251001", 100, 5000, 10000, 500)
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CalculateCalls with cache = %f, want %f", got, want)
	}
}

func TestCalculateCalls(t *testing.T) {
	calls := []models.LLMCall{
		{Model: "gemini-2.5-flash", InputTokens: 1000, OutputTokens: 500},
		{Model: "gemini-2.5-flash", InputTokens: 2000, OutputTokens: 1000},
	}
	got := CalculateCalls(calls)
	want := 0.00135
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CalculateCalls() = %f, want %f", got, want)
	}
}
