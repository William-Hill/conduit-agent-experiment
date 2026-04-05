# Full Scorecards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the Milestone 2 light scorecard with the full quantitative metrics and qualitative aggregation called for in PRD 7.17, using only data computable from existing run artifacts.

**Architecture:** Extend the existing `Scorecard` struct in `internal/evaluation/scorecard.go` with new fields; extend `GenerateScorecard` with additional counters in its existing loop plus a post-loop rate computation step; append three new sections to `FormatScorecard`. Add 5 optional `int` qualitative score fields to `models.Evaluation`. All changes backwards-compatible — zero-value defaults, `omitempty` JSON tags, existing tests stay untouched.

**Tech Stack:** Go 1.24, stdlib only (no new dependencies)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/models/evaluation.go` | Add 5 qualitative score fields to `Evaluation` struct |
| `internal/evaluation/scorecard.go` | Extend `Scorecard` struct, extend `GenerateScorecard` computation, extend `FormatScorecard` output |
| `internal/evaluation/scorecard_test.go` | Add tests for all new metrics and format sections |

No new files. Everything extends existing structures.

---

### Task 1: Feature branch and scaffolding (types only)

**Files:**
- Modify: `internal/models/evaluation.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Create feature branch from main**

```bash
git checkout main
git checkout -b feature/full-scorecards
```

- [ ] **Step 2: Add qualitative score fields to models.Evaluation**

In `internal/models/evaluation.go`, add 5 new fields to the `Evaluation` struct after the existing `Notes` field:

```go
	LintPass            bool        `json:"lint_pass,omitempty"`
	BuildPass           bool        `json:"build_pass,omitempty"`
	TestsPass           bool        `json:"tests_pass,omitempty"`
	ReviewScore         int         `json:"review_score,omitempty"`
	ArchitectureScore   int         `json:"architecture_score,omitempty"`
	Notes               string      `json:"notes,omitempty"`

	// Qualitative scores (1-5, 0 or omitted means not scored).
	// Typically filled in manually post-run by a human reviewer.
	ArchitecturalAlignment int `json:"architectural_alignment,omitempty"`
	RationaleClarity       int `json:"rationale_clarity,omitempty"`
	RetrievalUsefulness    int `json:"retrieval_usefulness,omitempty"`
	ReviewerConfidence     int `json:"reviewer_confidence,omitempty"`
	PatchReadability       int `json:"patch_readability,omitempty"`
```

- [ ] **Step 3: Extend the Scorecard struct with new fields**

In `internal/evaluation/scorecard.go`, update the `Scorecard` struct definition to add the new fields. The complete struct should be:

```go
// Scorecard aggregates evaluation results across multiple runs.
type Scorecard struct {
	TotalRuns           int            `json:"total_runs"`
	SuccessfulRuns      int            `json:"successful_runs"`
	PRsCreated          int            `json:"prs_created"`
	AvgFilesChanged     float64        `json:"avg_files_changed"`
	AvgDiffLines        float64        `json:"avg_diff_lines"`
	AvgLLMCalls         float64        `json:"avg_llm_calls"`
	SuccessByDifficulty map[string]int `json:"success_by_difficulty"`
	FailureModes        map[string]int `json:"failure_modes"`

	// Pass rates (denominator: TotalRuns).
	LintPassRate  float64 `json:"lint_pass_rate"`
	BuildPassRate float64 `json:"build_pass_rate"`
	TestsPassRate float64 `json:"tests_pass_rate"`

	// Iteration proxy (denominator: TotalRuns).
	AvgIterations float64 `json:"avg_iterations"`

	// Rate versions of existing count maps.
	AcceptanceRateByDifficulty map[string]float64 `json:"acceptance_rate_by_difficulty"`
	RejectionRateByFailureMode map[string]float64 `json:"rejection_rate_by_failure_mode"`

	// Qualitative aggregation (populated only when any run has scores).
	QualitativeScoreCount int                `json:"qualitative_score_count"`
	AvgQualitativeScores  map[string]float64 `json:"avg_qualitative_scores"`
}
```

- [ ] **Step 4: Verify the build is clean**

```bash
go build ./...
```

Expected: no errors. The new fields have zero-value defaults and don't break any existing code.

- [ ] **Step 5: Verify existing tests still pass**

```bash
go test ./internal/evaluation/ -v
```

Expected: all existing tests pass. New fields have zero-value defaults — existing assertions checking specific fields are unaffected.

- [ ] **Step 6: Commit**

```bash
git add internal/models/evaluation.go internal/evaluation/scorecard.go
git commit -m "feat: add qualitative score fields and extended scorecard struct"
```

---

### Task 2: Lint/Build/Tests pass rates

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Add this test to `internal/evaluation/scorecard_test.go` (append at the end of the file):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_PassRates -v
```

Expected: FAIL. All three pass rates will be `0` because `GenerateScorecard` doesn't populate them yet.

- [ ] **Step 3: Implement pass rate counters in GenerateScorecard**

In `internal/evaluation/scorecard.go`, modify `GenerateScorecard` to count pass events inside the existing loop and compute rates after the loop.

**Add new counter declarations** next to the existing `var totalFiles, totalDiff, totalLLM int` line:

```go
	var totalFiles, totalDiff, totalLLM int
	var lintPassCount, buildPassCount, testsPassCount int
```

**Inside the existing loop**, after the line `totalLLM += ev.LLMCalls`, add the pass-count increments:

```go
		sc.TotalRuns++
		totalFiles += ev.FilesChanged
		totalDiff += ev.DiffLines
		totalLLM += ev.LLMCalls

		if ev.LintPass {
			lintPassCount++
		}
		if ev.BuildPass {
			buildPassCount++
		}
		if ev.TestsPass {
			testsPassCount++
		}
```

**After the loop**, in the block that currently computes `sc.AvgFilesChanged` and friends, add the pass-rate assignments:

```go
	if sc.TotalRuns > 0 {
		sc.AvgFilesChanged = float64(totalFiles) / float64(sc.TotalRuns)
		sc.AvgDiffLines = float64(totalDiff) / float64(sc.TotalRuns)
		sc.AvgLLMCalls = float64(totalLLM) / float64(sc.TotalRuns)
		sc.LintPassRate = float64(lintPassCount) / float64(sc.TotalRuns)
		sc.BuildPassRate = float64(buildPassCount) / float64(sc.TotalRuns)
		sc.TestsPassRate = float64(testsPassCount) / float64(sc.TotalRuns)
	}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_PassRates -v
```

Expected: PASS.

- [ ] **Step 5: Run all existing evaluation tests to catch regressions**

```bash
go test ./internal/evaluation/ -v
```

Expected: all tests pass, including the original `TestGenerateScorecard`.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: add lint/build/tests pass rates to scorecard"
```

---

### Task 3: Average iterations

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/evaluation/scorecard_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_AvgIterations -v
```

Expected: FAIL. `AvgIterations` is 0 because it's not computed.

- [ ] **Step 3: Implement AvgIterations**

In `internal/evaluation/scorecard.go`, inside the `if sc.TotalRuns > 0 {` block (where the pass rates were added in Task 2), add one line to reuse the existing `totalLLM` counter:

```go
	if sc.TotalRuns > 0 {
		sc.AvgFilesChanged = float64(totalFiles) / float64(sc.TotalRuns)
		sc.AvgDiffLines = float64(totalDiff) / float64(sc.TotalRuns)
		sc.AvgLLMCalls = float64(totalLLM) / float64(sc.TotalRuns)
		sc.AvgIterations = float64(totalLLM) / float64(sc.TotalRuns)
		sc.LintPassRate = float64(lintPassCount) / float64(sc.TotalRuns)
		sc.BuildPassRate = float64(buildPassCount) / float64(sc.TotalRuns)
		sc.TestsPassRate = float64(testsPassCount) / float64(sc.TotalRuns)
	}
```

Note: `AvgIterations` and `AvgLLMCalls` hold the same number here — `AvgIterations` is the PRD-named metric, `AvgLLMCalls` is the M2 field kept for backwards compatibility.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_AvgIterations -v
```

Expected: PASS.

- [ ] **Step 5: Run all evaluation tests**

```bash
go test ./internal/evaluation/ -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: add AvgIterations metric to scorecard"
```

---

### Task 4: Acceptance rate by difficulty

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/evaluation/scorecard_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_AcceptanceRateByDifficulty -v
```

Expected: FAIL. `AcceptanceRateByDifficulty` is `nil`, all map lookups return zero.

- [ ] **Step 3: Implement runsByDifficulty counter and rate computation**

In `internal/evaluation/scorecard.go`:

**3a. Initialize the new map in the `sc := Scorecard{...}` literal at the top of `GenerateScorecard`:**

```go
	sc := Scorecard{
		SuccessByDifficulty:        make(map[string]int),
		FailureModes:               make(map[string]int),
		AcceptanceRateByDifficulty: make(map[string]float64),
	}
```

**3b. Declare the new counter next to the other local counters:**

```go
	var totalFiles, totalDiff, totalLLM int
	var lintPassCount, buildPassCount, testsPassCount int
	runsByDifficulty := make(map[string]int)
```

**3c. Inside the loop**, track every run by difficulty (regardless of success). Find the line that reads `sc.TotalRuns++` and add the difficulty increment right after it:

```go
		sc.TotalRuns++
		totalFiles += ev.FilesChanged
		totalDiff += ev.DiffLines
		totalLLM += ev.LLMCalls

		if ev.Difficulty != "" {
			runsByDifficulty[ev.Difficulty]++
		}

		if ev.LintPass {
			lintPassCount++
		}
```

**3d. After the `if sc.TotalRuns > 0 { ... }` block**, add a new post-loop block that computes the rates:

```go
	for diff, total := range runsByDifficulty {
		if total > 0 {
			sc.AcceptanceRateByDifficulty[diff] = float64(sc.SuccessByDifficulty[diff]) / float64(total)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_AcceptanceRateByDifficulty -v
```

Expected: PASS.

- [ ] **Step 5: Run all evaluation tests**

```bash
go test ./internal/evaluation/ -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: add AcceptanceRateByDifficulty to scorecard"
```

---

### Task 5: Rejection rate by failure mode

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/evaluation/scorecard_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_RejectionRateByFailureMode -v
```

Expected: FAIL. The field is nil/empty.

- [ ] **Step 3: Implement the rate computation**

In `internal/evaluation/scorecard.go`:

**3a. Initialize the map in the `sc := Scorecard{...}` literal:**

```go
	sc := Scorecard{
		SuccessByDifficulty:        make(map[string]int),
		FailureModes:               make(map[string]int),
		AcceptanceRateByDifficulty: make(map[string]float64),
		RejectionRateByFailureMode: make(map[string]float64),
	}
```

**3b. After the acceptance-rate post-loop block added in Task 4**, add the rejection-rate block:

```go
	totalFailed := sc.TotalRuns - sc.SuccessfulRuns
	if totalFailed > 0 {
		for mode, count := range sc.FailureModes {
			sc.RejectionRateByFailureMode[mode] = float64(count) / float64(totalFailed)
		}
	}
```

When `totalFailed == 0`, the map stays empty (never returns NaN).

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_RejectionRateByFailureMode -v
```

Expected: PASS (both sub-tests).

- [ ] **Step 5: Run all evaluation tests**

```bash
go test ./internal/evaluation/ -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: add RejectionRateByFailureMode to scorecard"
```

---

### Task 6: Qualitative scores aggregation

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/evaluation/scorecard_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_QualitativeScores -v
```

Expected: FAIL. `QualitativeScoreCount` is 0 and `AvgQualitativeScores` is nil.

- [ ] **Step 3: Implement qualitative aggregation**

In `internal/evaluation/scorecard.go`:

**3a. Initialize the map in the `sc := Scorecard{...}` literal:**

```go
	sc := Scorecard{
		SuccessByDifficulty:        make(map[string]int),
		FailureModes:               make(map[string]int),
		AcceptanceRateByDifficulty: make(map[string]float64),
		RejectionRateByFailureMode: make(map[string]float64),
		AvgQualitativeScores:       make(map[string]float64),
	}
```

**3b. Declare per-metric sum and count tracking** next to the other local counters near the top of `GenerateScorecard`:

```go
	var totalFiles, totalDiff, totalLLM int
	var lintPassCount, buildPassCount, testsPassCount int
	runsByDifficulty := make(map[string]int)

	qualSums := make(map[string]int)
	qualCounts := make(map[string]int)
```

**3c. Inside the existing loop**, after the existing increments, add qualitative score accumulation. This uses a small helper slice to stay DRY:

```go
		if ev.FailureMode != "" {
			sc.FailureModes[string(ev.FailureMode)]++
		}

		qualFields := []struct {
			name  string
			value int
		}{
			{"architectural_alignment", ev.ArchitecturalAlignment},
			{"rationale_clarity", ev.RationaleClarity},
			{"retrieval_usefulness", ev.RetrievalUsefulness},
			{"reviewer_confidence", ev.ReviewerConfidence},
			{"patch_readability", ev.PatchReadability},
		}
		scoredThisRun := false
		for _, q := range qualFields {
			if q.value != 0 {
				qualSums[q.name] += q.value
				qualCounts[q.name]++
				scoredThisRun = true
			}
		}
		if scoredThisRun {
			sc.QualitativeScoreCount++
		}
```

**3d. After the rejection-rate post-loop block added in Task 5**, add the qualitative average computation:

```go
	for metric, count := range qualCounts {
		if count > 0 {
			sc.AvgQualitativeScores[metric] = float64(qualSums[metric]) / float64(count)
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/evaluation/ -run TestGenerateScorecard_QualitativeScores -v
```

Expected: PASS (all three sub-tests).

- [ ] **Step 5: Run all evaluation tests**

```bash
go test ./internal/evaluation/ -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: aggregate qualitative scores in scorecard"
```

---

### Task 7: Extend FormatScorecard with new sections

**Files:**
- Modify: `internal/evaluation/scorecard_test.go`
- Modify: `internal/evaluation/scorecard.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/evaluation/scorecard_test.go`:

```go
func TestFormatScorecard_NewSections_Populated(t *testing.T) {
	sc := Scorecard{
		TotalRuns:       3,
		SuccessfulRuns:  2,
		AvgIterations:   7.5,
		LintPassRate:    0.67,
		BuildPassRate:   1.0,
		TestsPassRate:   0.33,
		AcceptanceRateByDifficulty: map[string]float64{
			"L1": 1.0,
			"L2": 0.5,
		},
		RejectionRateByFailureMode: map[string]float64{
			string(models.FailureHallucination): 1.0,
		},
		QualitativeScoreCount: 2,
		AvgQualitativeScores: map[string]float64{
			"architectural_alignment": 4.5,
			"rationale_clarity":       3.0,
		},
	}

	out := FormatScorecard(sc)

	checks := []string{
		"Avg Iterations",      // summary row for AvgIterations
		"Pass Rates",          // section header
		"Lint",
		"Build",
		"Tests",
		"Acceptance & Rejection Rates", // section header
		"Acceptance Rate",
		"Rejection Rate",
		"Qualitative Scores",           // section header
		"Scored runs: 2",
		"architectural_alignment",
		"rationale_clarity",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("FormatScorecard output missing %q", want)
		}
	}
}

func TestFormatScorecard_NewSections_Omitted(t *testing.T) {
	// Scorecard with no new metrics populated — new sections should be absent.
	sc := Scorecard{
		TotalRuns:      1,
		SuccessfulRuns: 1,
	}

	out := FormatScorecard(sc)

	// Qualitative Scores section should not render at all when QualitativeScoreCount == 0.
	if strings.Contains(out, "Qualitative Scores") {
		t.Error("FormatScorecard should not render Qualitative Scores section when no runs scored")
	}
	// Pass Rates section also should not render when LintPassRate/BuildPassRate/TestsPassRate all 0.
	if strings.Contains(out, "Pass Rates") {
		t.Error("FormatScorecard should not render Pass Rates section when all pass rates are 0")
	}
	// Acceptance & Rejection Rates section should not render when both maps are empty.
	if strings.Contains(out, "Acceptance & Rejection Rates") {
		t.Error("FormatScorecard should not render Acceptance & Rejection Rates section when both rate maps are empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/evaluation/ -run TestFormatScorecard_NewSections -v
```

Expected: FAIL — neither section exists yet.

- [ ] **Step 3: Extend FormatScorecard**

In `internal/evaluation/scorecard.go`, update `FormatScorecard`. Two changes:

**3a. Add `Avg Iterations` to the top Summary table.** Find the existing block:

```go
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Total Runs | %d |\n", sc.TotalRuns))
	sb.WriteString(fmt.Sprintf("| Successful Runs | %d |\n", sc.SuccessfulRuns))
	sb.WriteString(fmt.Sprintf("| PRs Created | %d |\n", sc.PRsCreated))
	sb.WriteString(fmt.Sprintf("| Avg Files Changed | %.2f |\n", sc.AvgFilesChanged))
	sb.WriteString(fmt.Sprintf("| Avg Diff Lines | %.2f |\n", sc.AvgDiffLines))
	sb.WriteString(fmt.Sprintf("| Avg LLM Calls | %.2f |\n", sc.AvgLLMCalls))
```

Add one line at the end of that block:

```go
	sb.WriteString(fmt.Sprintf("| Avg Iterations | %.2f |\n", sc.AvgIterations))
```

**3b. Add the three new sections after the existing `Failure Modes` section** (which ends with the closing `}` of the `if len(sc.FailureModes) > 0 {` block). Append this code before the final `return sb.String()`:

```go
	if sc.LintPassRate > 0 || sc.BuildPassRate > 0 || sc.TestsPassRate > 0 {
		sb.WriteString("\n## Pass Rates\n\n")
		sb.WriteString("| Check | Rate |\n")
		sb.WriteString("|-------|------|\n")
		sb.WriteString(fmt.Sprintf("| Lint  | %.2f |\n", sc.LintPassRate))
		sb.WriteString(fmt.Sprintf("| Build | %.2f |\n", sc.BuildPassRate))
		sb.WriteString(fmt.Sprintf("| Tests | %.2f |\n", sc.TestsPassRate))
	}

	if len(sc.AcceptanceRateByDifficulty) > 0 || len(sc.RejectionRateByFailureMode) > 0 {
		sb.WriteString("\n## Acceptance & Rejection Rates\n\n")

		if len(sc.AcceptanceRateByDifficulty) > 0 {
			sb.WriteString("| Difficulty | Acceptance Rate |\n")
			sb.WriteString("|------------|-----------------|\n")
			keys := make([]string, 0, len(sc.AcceptanceRateByDifficulty))
			for k := range sc.AcceptanceRateByDifficulty {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(fmt.Sprintf("| %s | %.2f |\n", k, sc.AcceptanceRateByDifficulty[k]))
			}
		}

		if len(sc.RejectionRateByFailureMode) > 0 {
			if len(sc.AcceptanceRateByDifficulty) > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("| Failure Mode | Rejection Rate |\n")
			sb.WriteString("|--------------|----------------|\n")
			keys := make([]string, 0, len(sc.RejectionRateByFailureMode))
			for k := range sc.RejectionRateByFailureMode {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(fmt.Sprintf("| %s | %.2f |\n", k, sc.RejectionRateByFailureMode[k]))
			}
		}
	}

	if sc.QualitativeScoreCount > 0 && len(sc.AvgQualitativeScores) > 0 {
		sb.WriteString("\n## Qualitative Scores\n\n")
		sb.WriteString(fmt.Sprintf("(Scored runs: %d)\n\n", sc.QualitativeScoreCount))
		sb.WriteString("| Metric | Avg (1-5) |\n")
		sb.WriteString("|--------|-----------|\n")
		keys := make([]string, 0, len(sc.AvgQualitativeScores))
		for k := range sc.AvgQualitativeScores {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("| %s | %.1f |\n", k, sc.AvgQualitativeScores[k]))
		}
	}
```

The `sort` package is already imported in `scorecard.go` (used by the existing format logic), so no import changes are needed.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/evaluation/ -run TestFormatScorecard -v
```

Expected: PASS (both the original `TestFormatScorecard` and the two new `TestFormatScorecard_NewSections_*` tests).

- [ ] **Step 5: Run all evaluation tests**

```bash
go test ./internal/evaluation/ -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: extend FormatScorecard with pass rates, acceptance/rejection, qualitative sections"
```

---

### Task 8: Full verification

**Files:** All files touched by previous tasks.

- [ ] **Step 1: Run the full evaluation package test suite**

```bash
go test ./internal/evaluation/ -v
```

Expected: ALL PASS — both existing M2 tests (`TestGenerateScorecard`, `TestGenerateScorecard_EmptyDir`, `TestFormatScorecard`, plus the metrics tests) and all new tests added in Tasks 2-7.

- [ ] **Step 2: Run the full project test suite**

```bash
go test ./...
```

Expected: ALL PASS. No regressions in any package.

- [ ] **Step 3: Run go vet**

```bash
go vet ./...
```

Expected: no issues.

- [ ] **Step 4: Verify clean build**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5: Final commit if cleanup was needed, otherwise skip**

Only if steps 1-5 revealed issues that needed fixing:

```bash
git add internal/evaluation/
git commit -m "fix: address issues found in full scorecards verification"
```
