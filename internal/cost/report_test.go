package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestWriteCostReport(t *testing.T) {
	dir := t.TempDir()
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 8000, OutputTokens: 1200},
		{Agent: "implementer", Model: "gemini-2.5-flash", InputTokens: 4500, OutputTokens: 2000},
	}
	budget := Budget{PipelineCap: 0.50}

	if err := WriteCostReport(dir, calls, budget); err != nil {
		t.Fatalf("WriteCostReport error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cost.json"))
	if err != nil {
		t.Fatalf("reading cost.json: %v", err)
	}

	var report CostReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parsing cost.json: %v", err)
	}

	if report.TotalCostUSD <= 0 {
		t.Error("expected positive total cost")
	}
	if report.TotalInputTokens != 12500 {
		t.Errorf("TotalInputTokens = %d, want 12500", report.TotalInputTokens)
	}
	if report.TotalOutputTokens != 3200 {
		t.Errorf("TotalOutputTokens = %d, want 3200", report.TotalOutputTokens)
	}
	if len(report.Steps) != 2 {
		t.Errorf("len(Steps) = %d, want 2", len(report.Steps))
	}
	if report.BudgetInfo.PipelineCapUSD != 0.50 {
		t.Errorf("PipelineCapUSD = %f, want 0.50", report.BudgetInfo.PipelineCapUSD)
	}
	if report.BudgetInfo.Exceeded {
		t.Error("budget should not be exceeded")
	}
}
