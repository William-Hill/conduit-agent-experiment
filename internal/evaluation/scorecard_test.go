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

func TestGenerateScorecard_AcceptanceRateByDifficulty(t *testing.T) {
	runsDir := t.TempDir()

	// L1: 2 runs, 1 success → 0.5 rate
	writeEvaluation(t, filepath.Join(runsDir, "run-l1-ok"), models.Evaluation{
		RunID:              "run-l1-ok",
		TaskID:             "task-l1-ok",
		Difficulty:         "L1",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-l1-fail"), models.Evaluation{
		RunID:              "run-l1-fail",
		TaskID:             "task-l1-fail",
		Difficulty:         "L1",
		ImplementerSuccess: false,
	})

	// L2: 1 run, 1 success → 1.0 rate
	writeEvaluation(t, filepath.Join(runsDir, "run-l2-ok"), models.Evaluation{
		RunID:              "run-l2-ok",
		TaskID:             "task-l2-ok",
		Difficulty:         "L2",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
	})

	// L3: 1 run, 0 successes → difficulty present in runsByDifficulty but not in SuccessByDifficulty.
	// Expected behavior: the rate map includes L3 with value 0.0 because we iterate runsByDifficulty keys.
	writeEvaluation(t, filepath.Join(runsDir, "run-l3-fail"), models.Evaluation{
		RunID:              "run-l3-fail",
		TaskID:             "task-l3-fail",
		Difficulty:         "L3",
		ImplementerSuccess: false,
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	if got := sc.AcceptanceRateByDifficulty["L1"]; got != 0.5 {
		t.Errorf("AcceptanceRateByDifficulty[L1] = %v, want 0.5", got)
	}
	if got := sc.AcceptanceRateByDifficulty["L2"]; got != 1.0 {
		t.Errorf("AcceptanceRateByDifficulty[L2] = %v, want 1.0", got)
	}
	if got, ok := sc.AcceptanceRateByDifficulty["L3"]; !ok || got != 0.0 {
		t.Errorf("AcceptanceRateByDifficulty[L3] = %v (present=%v), want 0.0 present", got, ok)
	}
}

func TestGenerateScorecard_RejectionRateByFailureMode(t *testing.T) {
	runsDir := t.TempDir()

	// 5 runs: 2 successful, 3 failed with different modes.
	writeEvaluation(t, filepath.Join(runsDir, "run-ok-1"), models.Evaluation{
		RunID:              "run-ok-1",
		TaskID:             "task-ok-1",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-ok-2"), models.Evaluation{
		RunID:              "run-ok-2",
		TaskID:             "task-ok-2",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-fail-1"), models.Evaluation{
		RunID:       "run-fail-1",
		TaskID:      "task-fail-1",
		FailureMode: models.FailureHallucination,
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-fail-2"), models.Evaluation{
		RunID:       "run-fail-2",
		TaskID:      "task-fail-2",
		FailureMode: models.FailureHallucination,
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-fail-3"), models.Evaluation{
		RunID:       "run-fail-3",
		TaskID:      "task-fail-3",
		FailureMode: models.FailureArchitectureDrift,
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	// Total failed = 5 - 2 = 3
	// Hallucination: 2/3 ≈ 0.6667
	// ArchitectureDrift: 1/3 ≈ 0.3333
	wantHallucination := 2.0 / 3.0
	wantDrift := 1.0 / 3.0

	if got := sc.RejectionRateByFailureMode[string(models.FailureHallucination)]; got != wantHallucination {
		t.Errorf("RejectionRateByFailureMode[hallucination] = %v, want %v", got, wantHallucination)
	}
	if got := sc.RejectionRateByFailureMode[string(models.FailureArchitectureDrift)]; got != wantDrift {
		t.Errorf("RejectionRateByFailureMode[drift] = %v, want %v", got, wantDrift)
	}
}

func TestGenerateScorecard_RejectionRateByFailureMode_NoFailures(t *testing.T) {
	runsDir := t.TempDir()

	writeEvaluation(t, filepath.Join(runsDir, "run-ok"), models.Evaluation{
		RunID:              "run-ok",
		TaskID:             "task-ok",
		ImplementerSuccess: true,
		VerifierPass:       true,
		ArchitectDecision:  "approve",
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	if len(sc.RejectionRateByFailureMode) != 0 {
		t.Errorf("RejectionRateByFailureMode should be empty when no runs failed, got %v", sc.RejectionRateByFailureMode)
	}
}

func TestGenerateScorecard_QualitativeScores_None(t *testing.T) {
	runsDir := t.TempDir()

	writeEvaluation(t, filepath.Join(runsDir, "run-1"), models.Evaluation{
		RunID:  "run-1",
		TaskID: "task-1",
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-2"), models.Evaluation{
		RunID:  "run-2",
		TaskID: "task-2",
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	if sc.QualitativeScoreCount != 0 {
		t.Errorf("QualitativeScoreCount = %d, want 0", sc.QualitativeScoreCount)
	}
	if len(sc.AvgQualitativeScores) != 0 {
		t.Errorf("AvgQualitativeScores should be empty, got %v", sc.AvgQualitativeScores)
	}
}

func TestGenerateScorecard_QualitativeScores_Partial(t *testing.T) {
	runsDir := t.TempDir()

	// Run 1: scores architectural_alignment and rationale_clarity only
	writeEvaluation(t, filepath.Join(runsDir, "run-1"), models.Evaluation{
		RunID:                  "run-1",
		TaskID:                 "task-1",
		ArchitecturalAlignment: 4,
		RationaleClarity:       3,
	})

	// Run 2: scores architectural_alignment and patch_readability only
	writeEvaluation(t, filepath.Join(runsDir, "run-2"), models.Evaluation{
		RunID:                  "run-2",
		TaskID:                 "task-2",
		ArchitecturalAlignment: 5,
		PatchReadability:       4,
	})

	// Run 3: scores nothing
	writeEvaluation(t, filepath.Join(runsDir, "run-3"), models.Evaluation{
		RunID:  "run-3",
		TaskID: "task-3",
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	// 2 runs had at least one qualitative score (runs 1 and 2).
	if sc.QualitativeScoreCount != 2 {
		t.Errorf("QualitativeScoreCount = %d, want 2", sc.QualitativeScoreCount)
	}

	// architectural_alignment: (4+5)/2 = 4.5
	if got := sc.AvgQualitativeScores["architectural_alignment"]; got != 4.5 {
		t.Errorf("AvgQualitativeScores[architectural_alignment] = %v, want 4.5", got)
	}
	// rationale_clarity: only run 1 scored → 3/1 = 3
	if got := sc.AvgQualitativeScores["rationale_clarity"]; got != 3.0 {
		t.Errorf("AvgQualitativeScores[rationale_clarity] = %v, want 3.0", got)
	}
	// patch_readability: only run 2 scored → 4/1 = 4
	if got := sc.AvgQualitativeScores["patch_readability"]; got != 4.0 {
		t.Errorf("AvgQualitativeScores[patch_readability] = %v, want 4.0", got)
	}
	// retrieval_usefulness: no one scored → should be absent from map
	if _, ok := sc.AvgQualitativeScores["retrieval_usefulness"]; ok {
		t.Errorf("AvgQualitativeScores should not contain retrieval_usefulness when no run scored it")
	}
	// reviewer_confidence: no one scored → should be absent from map
	if _, ok := sc.AvgQualitativeScores["reviewer_confidence"]; ok {
		t.Errorf("AvgQualitativeScores should not contain reviewer_confidence when no run scored it")
	}
}

func TestGenerateScorecard_QualitativeScores_All(t *testing.T) {
	runsDir := t.TempDir()

	writeEvaluation(t, filepath.Join(runsDir, "run-a"), models.Evaluation{
		RunID:                  "run-a",
		TaskID:                 "task-a",
		ArchitecturalAlignment: 5,
		RationaleClarity:       4,
		RetrievalUsefulness:    3,
		ReviewerConfidence:     4,
		PatchReadability:       5,
	})
	writeEvaluation(t, filepath.Join(runsDir, "run-b"), models.Evaluation{
		RunID:                  "run-b",
		TaskID:                 "task-b",
		ArchitecturalAlignment: 3,
		RationaleClarity:       2,
		RetrievalUsefulness:    5,
		ReviewerConfidence:     2,
		PatchReadability:       3,
	})

	sc, err := GenerateScorecard(runsDir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}

	if sc.QualitativeScoreCount != 2 {
		t.Errorf("QualitativeScoreCount = %d, want 2", sc.QualitativeScoreCount)
	}

	expected := map[string]float64{
		"architectural_alignment": 4.0,
		"rationale_clarity":       3.0,
		"retrieval_usefulness":    4.0,
		"reviewer_confidence":     3.0,
		"patch_readability":       4.0,
	}
	for metric, want := range expected {
		if got := sc.AvgQualitativeScores[metric]; got != want {
			t.Errorf("AvgQualitativeScores[%s] = %v, want %v", metric, got, want)
		}
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
