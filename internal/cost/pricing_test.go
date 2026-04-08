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

func TestCalculateUnknownModel(t *testing.T) {
	got := Calculate("unknown-model", 1000, 500)
	if got != 0.0 {
		t.Errorf("Calculate(unknown, ...) = %f, want 0.0", got)
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
