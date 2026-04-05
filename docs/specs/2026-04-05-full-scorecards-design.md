# Full Scorecards Design Spec

**Issue:** #5 — Milestone 3: Extended reporting and experiment writeup (sub-project A)
**Date:** 2026-04-05
**Scope:** `internal/models/evaluation.go`, `internal/evaluation/scorecard.go`, `internal/evaluation/scorecard_test.go`

## Goal

Extend the Milestone 2 light scorecard with the full quantitative and qualitative metrics called for in PRD section 7.17. Ship a tight, computable-from-existing-data slice; leave doors open for future automation without committing to it now.

## Design Decisions

Three decisions locked in during brainstorming:

1. **Qualitative metrics: hybrid (schema + manual entry, aggregate when present).** Add fields to `models.Evaluation` for the 5 qualitative metrics listed in the issue. Scorecard aggregates them when any run scores them; output section is omitted when none do. No LLM-as-judge automation in this sub-project.
2. **Quantitative scope: only what's computable from existing data.** Add pass rates for lint/build/tests, average iterations (LLM call count as proxy), and rate versions of the existing success-by-difficulty / failure-mode maps. **Explicitly dropped:** time to first patch draft, human review time. Both require new instrumentation that benefits from being designed after seeing pilot data.
3. **Type strategy: extend `Scorecard` in place.** No new `FullScorecard` type. All additions are backwards-compatible fields on the existing struct. One CLI command, one file, one format function.

## Data Model Changes

### `internal/models/evaluation.go`

Add 5 optional integer score fields to the `Evaluation` struct (alongside the existing `LintPass`/`BuildPass`/`TestsPass`/`ReviewScore`/`ArchitectureScore`/`Notes` deferred fields):

```go
// Qualitative scores (1-5, 0 or omitted means not scored).
// Typically filled in manually post-run by a human reviewer.
ArchitecturalAlignment int `json:"architectural_alignment,omitempty"`
RationaleClarity       int `json:"rationale_clarity,omitempty"`
RetrievalUsefulness    int `json:"retrieval_usefulness,omitempty"`
ReviewerConfidence     int `json:"reviewer_confidence,omitempty"`
PatchReadability       int `json:"patch_readability,omitempty"`
```

**Scale:** 1 (poor) to 5 (excellent). **Zero means "not scored"** — the scorecard ignores zero values, keeping the existing `omitempty` pattern M2 uses.

### `internal/evaluation/scorecard.go`

Extend the `Scorecard` struct with new fields. All additions are backwards-compatible; existing JSON consumers see zero-value defaults.

```go
// Pass rates (denominator: TotalRuns).
LintPassRate  float64 `json:"lint_pass_rate"`
BuildPassRate float64 `json:"build_pass_rate"`
TestsPassRate float64 `json:"tests_pass_rate"`

// Rate versions of existing count maps (kept alongside, not replacing).
AcceptanceRateByDifficulty map[string]float64 `json:"acceptance_rate_by_difficulty"`
RejectionRateByFailureMode map[string]float64 `json:"rejection_rate_by_failure_mode"`

// Qualitative aggregation (populated only when any run has scores).
QualitativeScoreCount int                `json:"qualitative_score_count"`
AvgQualitativeScores  map[string]float64 `json:"avg_qualitative_scores"`
```

Existing fields (`TotalRuns`, `SuccessfulRuns`, `PRsCreated`, `AvgFilesChanged`, `AvgDiffLines`, `AvgLLMCalls`, `SuccessByDifficulty`, `FailureModes`) are unchanged.

## Computation Logic

`GenerateScorecard` gains new counters and a small amount of additional work in its existing loop, plus a post-loop rate-computation step. All logic stays in `scorecard.go`.

### Pass rates

Denominator is `TotalRuns`. A run where lint wasn't executed (zero-value `false`) counts the same as a run where lint failed. This is the honest summary given the current bool schema. Tri-state promotion (run/not-run/failed) is out of scope.

```
LintPassRate  = count(runs where LintPass is true)  / TotalRuns
BuildPassRate = count(runs where BuildPass is true) / TotalRuns
TestsPassRate = count(runs where TestsPass is true) / TotalRuns
```

When `TotalRuns == 0`, all rates are `0.0`.

### Acceptance rate by difficulty

Needs a new counter: `runsByDifficulty map[string]int` accumulated during the loop. For every observed difficulty (each key in `runsByDifficulty`):

```
AcceptanceRateByDifficulty[k] = SuccessByDifficulty[k] / runsByDifficulty[k]
```

This ensures difficulties with zero successes are included in `AcceptanceRateByDifficulty` and yield `0.00`. Zero-total keys are omitted (no division by zero).

### Rejection rate by failure mode

Denominator is `TotalRuns - SuccessfulRuns` (total failed runs). For each mode key present in `FailureModes`:

```
RejectionRateByFailureMode[k] = FailureModes[k] / (TotalRuns - SuccessfulRuns)
```

If zero runs failed, the map is empty (no entries). Never returns NaN.

### Qualitative aggregation

Two independent things get tracked:

1. **Per-metric sum and count.** Each of the 5 qualitative fields has its own running sum and count. During the main loop, for each run, for each qualitative field: if non-zero, add to that field's sum and increment that field's count.
2. **Run-level "any score" count.** A separate counter increments once per run if that run had at least one non-zero qualitative field. This becomes `QualitativeScoreCount`.

After the loop, for each metric with `count > 0`:

```
AvgQualitativeScores["architectural_alignment"] = sum / count
```

Metrics with zero count are absent from the map (not present as `0.0`). This means `AvgQualitativeScores` may contain 0-5 entries depending on what was scored.

If no run scored any qualitative metric at all: `AvgQualitativeScores` is empty and `QualitativeScoreCount == 0`. These two must be consistent — `QualitativeScoreCount == 0` implies `len(AvgQualitativeScores) == 0` and vice versa.

## Format Output

`FormatScorecard` gains three new sections, appended after the existing Summary / Success by Difficulty / Failure Modes sections. **Sections with empty data are omitted entirely** — matching the pattern the existing `FormatScorecard` already uses for the optional maps.

The existing `AvgLLMCalls` field is rendered as "Avg Iterations" in the Summary table (alongside its separate "Avg LLM Calls" row for backwards compatibility).

Example output (all sections populated):

```
## Pass Rates

| Check | Rate |
|-------|------|
| Lint  | 0.67 |
| Build | 1.00 |
| Tests | 0.83 |

## Acceptance & Rejection Rates

| Difficulty | Acceptance Rate |
|------------|-----------------|
| level_1    | 0.80            |
| level_2    | 0.50            |

| Failure Mode        | Rejection Rate |
|---------------------|----------------|
| architecture_drift  | 0.40           |
| retrieval_failure   | 0.20           |

## Qualitative Scores

(Scored runs: 3)

| Metric                   | Avg (1-5) |
|--------------------------|-----------|
| architectural_alignment  | 4.0       |
| rationale_clarity        | 3.7       |
| retrieval_usefulness     | 3.0       |
```

When `QualitativeScoreCount == 0`, the entire "Qualitative Scores" section is absent from the rendered markdown (no stub row, no empty table).

## Testing Strategy

Tests go in the existing `internal/evaluation/scorecard_test.go`, following the pattern M2 established: construct `models.Evaluation` values in-memory, write them to temp dirs as JSON, call `GenerateScorecard`, assert on both the struct and the `FormatScorecard` output.

New test functions:

- **`TestGenerateScorecard_PassRates`** — fixture with mixed `LintPass`/`BuildPass`/`TestsPass` states across multiple runs; asserts rates computed against `TotalRuns`.
- **`TestGenerateScorecard_AcceptanceRateByDifficulty`** — fixture with multiple difficulties and mixed success; asserts per-difficulty rates including the edge case where a difficulty has zero successes.
- **`TestGenerateScorecard_RejectionRateByFailureMode`** — fixture with multiple failure modes; asserts rates and the zero-failed-runs edge case (empty map, no NaN).
- **`TestGenerateScorecard_QualitativeScores`** — three sub-cases:
  - No runs have qualitative fields → `QualitativeScoreCount == 0`, `AvgQualitativeScores` empty
  - Partial scoring (some runs score some metrics) → per-metric averages computed only over runs that scored that specific metric
  - Full scoring (all runs score all metrics) → straight averages
- **`TestFormatScorecard_NewSections`** — verifies the new sections appear when data is present, and that the Qualitative Scores section is fully absent when `QualitativeScoreCount == 0`.

**Existing M2 scorecard tests stay untouched.** New fields have zero-value defaults that won't affect existing assertions, and `omitempty` JSON tags on the new qualitative scoring fields keep existing `evaluation.json` fixtures valid.

## Scope Guardrails

Explicitly out of scope for this sub-project:

- **No new CLI command.** `go run ./cmd/experiment scorecard` already exists and picks up the new fields automatically via the shared `FormatScorecard` function.
- **No LLM-as-judge scoring.** Qualitative fields are filled manually by editing `evaluation.json` post-run. Automation is a separate future issue.
- **No new `Run` model timestamps.** "Time to first patch draft" and "human review time" are deferred — they were explicitly dropped in the quantitative scope decision.
- **No changes to the orchestrator.** The scorecard reads files after runs complete; no runtime instrumentation needed.
- **No tri-state lint/build/test state.** "Not run" and "failed" look identical in the current bool schema; we accept that and use `TotalRuns` as the honest denominator.
- **No aggregate failure taxonomy work.** Sub-project B handles deeper taxonomy aggregation.
- **No draft PR wiring.** Sub-project C handles orchestrator integration.
- **No experiment writeup assets.** Sub-project D handles documentation.

## Constraints

- No new external dependencies — stdlib only, matching the existing package
- Backwards-compatible JSON (new fields only, `omitempty` where appropriate)
- Existing M2 tests must continue to pass unchanged
- TDD: tests first for each new metric