package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func writeEvaluation(t *testing.T, dir string, ev models.Evaluation) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal evaluation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evaluation.json"), data, 0o644); err != nil {
		t.Fatalf("write evaluation.json: %v", err)
	}
}

func TestGenerateScorecard(t *testing.T) {
	runsDir := t.TempDir()

	// Run 1: success
	successRun := models.Evaluation{
		RunID:              "run-task-001-20260404-120000",
		TaskID:             "task-001",
		Difficulty:         "L1",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
		PRCreated:          true,
		FilesChanged:       4,
		DiffLines:          80,
		LLMCalls:           6,
	}
	writeEvaluation(t, filepath.Join(runsDir, "run-task-001-20260404-120000"), successRun)

	// Run 2: failure
	failureRun := models.Evaluation{
		RunID:              "run-task-002-20260404-130000",
		TaskID:             "task-002",
		Difficulty:         "L2",
		ImplementerSuccess: false,
		VerifierPass:       false,
		PRCreated:          false,
		FilesChanged:       2,
		DiffLines:          20,
		LLMCalls:           10,
		FailureMode:        models.FailureHallucination,
	}
	writeEvaluation(t, filepath.Join(runsDir, "run-task-002-20260404-130000"), failureRun)

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	if sc.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", sc.TotalRuns)
	}
	if sc.SuccessfulRuns != 1 {
		t.Errorf("SuccessfulRuns = %d, want 1", sc.SuccessfulRuns)
	}
	if sc.PRsCreated != 1 {
		t.Errorf("PRsCreated = %d, want 1", sc.PRsCreated)
	}

	wantAvgFiles := (4.0 + 2.0) / 2.0
	if sc.AvgFilesChanged != wantAvgFiles {
		t.Errorf("AvgFilesChanged = %v, want %v", sc.AvgFilesChanged, wantAvgFiles)
	}

	wantAvgDiff := (80.0 + 20.0) / 2.0
	if sc.AvgDiffLines != wantAvgDiff {
		t.Errorf("AvgDiffLines = %v, want %v", sc.AvgDiffLines, wantAvgDiff)
	}

	wantAvgLLM := (6.0 + 10.0) / 2.0
	if sc.AvgLLMCalls != wantAvgLLM {
		t.Errorf("AvgLLMCalls = %v, want %v", sc.AvgLLMCalls, wantAvgLLM)
	}

	if sc.SuccessByDifficulty["L1"] != 1 {
		t.Errorf("SuccessByDifficulty[L1] = %d, want 1", sc.SuccessByDifficulty["L1"])
	}
	if _, ok := sc.SuccessByDifficulty["L2"]; ok {
		t.Errorf("SuccessByDifficulty[L2] should not be set for a failed run")
	}

	if sc.FailureModes[string(models.FailureHallucination)] != 1 {
		t.Errorf("FailureModes[%s] = %d, want 1", models.FailureHallucination, sc.FailureModes[string(models.FailureHallucination)])
	}
}

func TestGenerateScorecard_EmptyDir(t *testing.T) {
	runsDir := t.TempDir()

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() on empty dir error: %v", err)
	}
	if sc.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", sc.TotalRuns)
	}
}

func TestGenerateScorecard_PassRates(t *testing.T) {
	runsDir := t.TempDir()

	// Run 1: lint + build + tests all pass
	writeEvaluation(t, filepath.Join(runsDir, "run-a"), models.Evaluation{
		RunID:     "run-a",
		TaskID:    "task-a",
		LintPass:  true,
		BuildPass: true,
		TestsPass: true,
	})

	// Run 2: only build passes
	writeEvaluation(t, filepath.Join(runsDir, "run-b"), models.Evaluation{
		RunID:     "run-b",
		TaskID:    "task-b",
		LintPass:  false,
		BuildPass: true,
		TestsPass: false,
	})

	// Run 3: nothing passes
	writeEvaluation(t, filepath.Join(runsDir, "run-c"), models.Evaluation{
		RunID:     "run-c",
		TaskID:    "task-c",
		LintPass:  false,
		BuildPass: false,
		TestsPass: false,
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	// Denominator is TotalRuns = 3.
	wantLint := 1.0 / 3.0
	wantBuild := 2.0 / 3.0
	wantTests := 1.0 / 3.0

	if sc.LintPassRate != wantLint {
		t.Errorf("LintPassRate = %v, want %v", sc.LintPassRate, wantLint)
	}
	if sc.BuildPassRate != wantBuild {
		t.Errorf("BuildPassRate = %v, want %v", sc.BuildPassRate, wantBuild)
	}
	if sc.TestsPassRate != wantTests {
		t.Errorf("TestsPassRate = %v, want %v", sc.TestsPassRate, wantTests)
	}
}

func TestGenerateScorecard_AvgIterations(t *testing.T) {
	runsDir := t.TempDir()

	writeEvaluation(t, filepath.Join(runsDir, "run-1"), models.Evaluation{
		RunID:    "run-1",
		TaskID:   "task-1",
		LLMCalls: 4,
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-2"), models.Evaluation{
		RunID:    "run-2",
		TaskID:   "task-2",
		LLMCalls: 10,
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-3"), models.Evaluation{
		RunID:    "run-3",
		TaskID:   "task-3",
		LLMCalls: 7,
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	wantAvg := (4.0 + 10.0 + 7.0) / 3.0
	if sc.AvgIterations != wantAvg {
		t.Errorf("AvgIterations = %v, want %v", sc.AvgIterations, wantAvg)
	}
}

func TestFormatScorecard(t *testing.T) {
	sc := Scorecard{
		TotalRuns:       2,
		SuccessfulRuns:  1,
		PRsCreated:      1,
		AvgFilesChanged: 3.0,
		AvgDiffLines:    50.0,
		AvgLLMCalls:     8.0,
		SuccessByDifficulty: map[string]int{
			"L1": 1,
		},
		FailureModes: map[string]int{
			string(models.FailureHallucination): 1,
		},
	}

	out := FormatScorecard(sc)

	checks := []string{
		"Total Runs",
		"2",
		"Successful Runs",
		"1",
		"PRs Created",
		"Avg Files Changed",
		"Avg Diff Lines",
		"Avg LLM Calls",
		"L1",
		string(models.FailureHallucination),
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("FormatScorecard output missing %q", want)
		}
	}
}
