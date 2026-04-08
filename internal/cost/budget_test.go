package cost

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCheckStepUnderBudget(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{"archivist": 0.10}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 1000, OutputTokens: 500},
	}
	if err := b.CheckStep("archivist", calls); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckStepOverBudget(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{"archivist": 0.0001}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckStep("archivist", calls); err == nil {
		t.Error("expected budget exceeded error")
	}
}

func TestCheckStepNoCap(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckStep("archivist", calls); err != nil {
		t.Errorf("no cap should not error: %v", err)
	}
}

func TestCheckTotalOverBudget(t *testing.T) {
	b := Budget{PipelineCap: 0.001}
	calls := []models.LLMCall{
		{Model: "claude-haiku-4-5-20251001", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckTotal(calls); err == nil {
		t.Error("expected pipeline budget exceeded error")
	}
}

func TestCheckTotalNoCap(t *testing.T) {
	b := Budget{}
	calls := []models.LLMCall{
		{Model: "claude-haiku-4-5-20251001", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckTotal(calls); err != nil {
		t.Errorf("no cap should not error: %v", err)
	}
}

func TestLoadBudgetFromEnv(t *testing.T) {
	t.Setenv("PIPELINE_MAX_COST", "0.50")
	t.Setenv("ARCHIVIST_MAX_COST", "0.10")
	t.Setenv("IMPL_MAX_COST", "0.25")

	b := LoadBudget()
	if b.PipelineCap != 0.50 {
		t.Errorf("PipelineCap = %f, want 0.50", b.PipelineCap)
	}
	if b.StepCaps["archivist"] != 0.10 {
		t.Errorf("archivist cap = %f, want 0.10", b.StepCaps["archivist"])
	}
	if b.StepCaps["implementer"] != 0.25 {
		t.Errorf("implementer cap = %f, want 0.25", b.StepCaps["implementer"])
	}
}
