package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeSummary(t *testing.T, dir, backend string, cost float64, iterations int, budgetExceeded bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"backend":"` + backend +
		`","estimated_cost_usd":` + strconv.FormatFloat(cost, 'f', 4, 64) +
		`,"iterations":` + strconv.Itoa(iterations) +
		`,"budget_exceeded":` + strconv.FormatBool(budgetExceeded) +
		`,"input_tokens":100,"output_tokens":50,"hallucinated_symbols":0}`
	if err := os.WriteFile(filepath.Join(dir, "run-summary.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzePartitionsByBackend(t *testing.T) {
	root := t.TempDir()
	writeSummary(t, filepath.Join(root, "a", "r1"), "anthropic:claude-haiku-4-5", 0.05, 5, false)
	writeSummary(t, filepath.Join(root, "a", "r2"), "anthropic:claude-haiku-4-5", 0.06, 6, false)
	writeSummary(t, filepath.Join(root, "b", "r1"), "aider:openrouter/qwen", 0.00, 1, false)
	writeSummary(t, filepath.Join(root, "b", "r2"), "aider:openrouter/qwen", 0.00, 1, true)

	report, err := analyze(root)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(report.Arms) != 2 {
		t.Errorf("got %d arms, want 2", len(report.Arms))
	}
	anthropic := report.Arm("anthropic:claude-haiku-4-5")
	if anthropic == nil || anthropic.Runs != 2 {
		t.Errorf("anthropic arm should have 2 runs, got %v", anthropic)
	}
	if anthropic.MeanCost < 0.054 || anthropic.MeanCost > 0.056 {
		t.Errorf("mean cost = %f, want ~0.055", anthropic.MeanCost)
	}
	aider := report.Arm("aider:openrouter/qwen")
	if aider == nil || aider.Runs != 2 {
		t.Errorf("aider arm should have 2 runs, got %v", aider)
	}
	if aider.SuccessRate != 0.5 {
		t.Errorf("aider success rate = %f, want 0.5", aider.SuccessRate)
	}
}

func TestAnalyzeSkipsMalformedJSON(t *testing.T) {
	root := t.TempDir()
	// One good summary, one malformed file.
	writeSummary(t, filepath.Join(root, "good"), "anthropic", 0.05, 5, false)
	badDir := filepath.Join(root, "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "run-summary.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := analyze(root)
	if err != nil {
		t.Fatalf("analyze should not fail on bad JSON: %v", err)
	}
	if len(report.Arms) != 1 {
		t.Errorf("expected 1 arm (the good one), got %d", len(report.Arms))
	}
}

func TestAnalyzeSkipsMissingBackendField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Missing backend field entirely.
	if err := os.WriteFile(filepath.Join(dir, "run-summary.json"),
		[]byte(`{"estimated_cost_usd":0.05,"iterations":3,"budget_exceeded":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := analyze(root)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(report.Arms) != 0 {
		t.Errorf("expected 0 arms (missing backend skipped), got %d", len(report.Arms))
	}
}
