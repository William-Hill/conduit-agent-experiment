# Internal Code Reviewer Design

**Issue:** #33
**Date:** 2026-04-09
**Status:** Accepted

## Overview

Add an internal code-review gate between the implementer and the PR push. The gate re-runs `go build ./...` and `go vet ./...` against the cloned target repo (ignoring the implementer's self-reported build status), then makes one Gemini Flash call to check semantic concerns the compiler can't see. On rejection, the pipeline re-runs the implementer once with structured feedback and re-reviews; on a second rejection, the pipeline halts before creating a PR.

The motivation is a persistent failure mode observed across every pipeline run so far: hallucinated symbols and unfinished stubs making it all the way to a published PR. The implementer's system prompt already tells Haiku to run `go build` inside its tool loop (`internal/implementer/agent.go:16-24`), but we cannot trust the agent's self-report — this gate is an external verification layer.

## Design Decisions

- **Deterministic checks drive the primary verdict** — `go build ./...` and `go vet ./...` are the reliable signal for hallucinated symbols, and they're free. The LLM is a cheap second layer for things execution can't verify (stubs, unrelated changes, ignored requirements).
- **Single pre-push gate, not a multi-stage review** — compile → vet → semantic LLM, short-circuiting on the first failure. Keeps the gate fast and the feedback focused on one category at a time.
- **Re-run the implementer on rejection** — feedback is appended to the original plan under a `## Reviewer Feedback` section and `implementer.RunAgent` is invoked again. Mirrors the existing plan-reviewer retry at `cmd/implementer/main.go:155-172`. Capped at one retry to prevent runaway cost.
- **Budget is cumulative across the original run + retry** — the retry reuses `IMPL_MAX_COST`. Token counts from both runs are merged into the primary `Result` so `run-summary.json` reflects total implementer spend.
- **Final rejection halts the pipeline with `os.Exit(1)`** — no PR is created; the rejection verdict is written to `run-summary.json` under a new `code_review` field for dashboard visibility.
- **No feature flag** — the reviewer runs every time. If incident response requires disabling it, a small revert PR is clearer than a drift-prone env var.
- **New `internal/codereviewer/` package** — keeps a clean separation from the existing plan reviewer at `internal/planner/reviewer.go`. No naming collision, one job per package.
- **`go test` is intentionally excluded** — Conduit tests are flaky in the local agent environment (per observed failure modes) and can take minutes. Tests are CI's job; this gate is pre-push.

## Architecture

### Pipeline placement

The new gate lives in `cmd/implementer/main.go` as step 9a, between existing step 9 (change detection) and step 10 (PR upsert):

```
 8. implementer.RunAgent            ← produces file changes
 9. git diff / git status           ← confirms changes exist
 9a. codereviewer.Review (NEW)      ← verdict gate
     ├─ go build ./...              (deterministic)
     ├─ go vet ./...                (deterministic, skipped if build fails)
     └─ Gemini Flash semantic call  (skipped if build or vet fails)
     → reject? re-run implementer once with feedback, re-review
     → still reject? write run-summary.code_review, os.Exit(1)
10. UpsertBranchAndPR               ← only runs if approved
```

### Package layout

```
internal/codereviewer/
├── types.go          # Verdict, CheckResult
├── checks.go         # RunBuild, RunVet
├── reviewer.go       # Review (public entrypoint) + Gemini Flash call
└── reviewer_test.go  # unit + integration tests
```

### Public API

```go
// Review runs the deterministic and semantic gates against the current
// working tree in repoDir. It does not mutate the repo.
//
// Returns a Verdict describing the outcome. A non-nil error means the
// gate itself failed to run (not that the code was rejected) — callers
// should distinguish Verdict.Approved from err.
func Review(
    ctx context.Context,
    geminiKey string,
    repoDir string,
    issue *github.Issue,
    plan *planner.ImplementationPlan,
    dossier *archivist.Dossier,
) (*Verdict, error)
```

The `issue` parameter is `*github.Issue` (the enriched issue from `adapter.GetIssue`, bound as `fullIssue` in `cmd/implementer/main.go:108`) — not `*triage.RankedIssue`, because we need the issue body and `triage.RankedIssue` doesn't carry one.

`Review` is side-effect-free on the repo *state that matters*: it runs build, runs vet, collects the diff, calls Gemini Flash once, returns. It does not commit, push, or write files. The one caveat is that collecting the diff for untracked files requires `git add -N .` (intent-to-add), which mutates the git index but not the working tree or any committed content — the caller's subsequent commit in `UpsertBranchAndPR` is unaffected. The caller (`cmd/implementer/main.go`) owns orchestration: retry, budget merging, artifact writes, exit codes.

### Types

```go
// Verdict is the final outcome of a code review.
type Verdict struct {
    Approved bool   `json:"approved"`
    // Category indicates which gate failed, if any.
    // One of: "build", "vet", "semantic", "".
    Category string `json:"category,omitempty"`
    // Summary is a human-readable one-liner for logs/PRs/dashboards.
    Summary string `json:"summary"`
    // Feedback is the structured message passed back to the implementer
    // on retry. Concatenation of build errors, vet errors, and LLM
    // semantic feedback as applicable.
    Feedback string `json:"feedback,omitempty"`

    // Diagnostics — populated even on approval, for the artifact.
    BuildOutput    string `json:"build_output,omitempty"`
    VetOutput      string `json:"vet_output,omitempty"`
    SemanticResult string `json:"semantic_result,omitempty"`

    // Cost telemetry for the semantic LLM call (zero if skipped).
    InputTokens  int     `json:"input_tokens"`
    OutputTokens int     `json:"output_tokens"`
    CostUSD      float64 `json:"cost_usd"`
}

// CheckResult is the outcome of a single deterministic check.
type CheckResult struct {
    Passed   bool
    ExitCode int
    Output   string // combined stdout+stderr, truncated to 16 KiB
}
```

## Components

### `checks.go` — deterministic checks

Two functions, both thin wrappers over `exec.CommandContext`:

```go
func RunBuild(ctx context.Context, repoDir string) (*CheckResult, error)
func RunVet(ctx context.Context, repoDir string) (*CheckResult, error)
```

**Execution details:**

- 2-minute hard timeout via `context.WithTimeout` — matches the implementer's `run_command` tool timeout at `internal/implementer/tools.go:268`.
- Minimal environment: `PATH`, `HOME`, `GOPATH`, `GOROOT`, `TMPDIR` only — identical to the allowlist at `internal/implementer/tools.go:275-281`. Prevents a compromised target repo's build script from exfiltrating `ANTHROPIC_API_KEY` / `GOOGLE_API_KEY`.
- Combined stdout + stderr, capped at 16 KiB so pathological build logs can't blow up the LLM prompt or run-summary artifact.
- `Passed = (err == nil && exitCode == 0)`. A `*exec.ExitError` is non-fatal — it means the check failed, not that the runner broke. Only non-`ExitError` errors (e.g., `go` not on PATH, context deadline) bubble up as `error`.

### `reviewer.go` — semantic LLM layer

Reached only when build and vet both pass. Modeled closely on the existing `planner.ReviewPlan` at `internal/planner/reviewer.go` — same SDK (`google.golang.org/genai`), same JSON-mode output, same `llmutil.CleanJSON` path — one pattern to maintain, not two.

**Prompt construction:**

1. Issue title + body (from the `*github.Issue` parameter)
2. Plan markdown (`plan.Markdown`)
3. Dossier summary + approach (so the reviewer knows *what should have changed and why*)
4. Combined diff from `repoDir`, truncated to ~32 KiB — produced by running `git add -N .` followed by `git diff HEAD`. The `-N` (intent-to-add) marks untracked files as "will be added" so they appear in `git diff` output alongside tracked modifications. Without this trick, brand-new files (the common case for new packages) would be invisible to the reviewer.
5. List of touched files from `git status --porcelain` (belt-and-suspenders: gives the LLM an unambiguous file list even when the diff is truncated)

**System prompt:**

```
You are a code review engineer. You receive a GitHub issue, a plan, a
research dossier, and a git diff of an attempted implementation. The
code already compiles (go build) and passes go vet.

Check ONLY these semantic concerns:
1. Does the diff actually address the issue in the plan?
2. Are there obvious stubs, TODO/FIXME markers, or unfinished code
   ("... rest of implementation" etc.)?
3. Are there changes to files unrelated to the plan?
4. Does the diff drop or ignore requirements the plan explicitly called
   out?

Do NOT flag style, naming, test coverage, or "could be cleaner"
concerns — those are for CI and human review. Be strict about stubs
and missing work; lenient about everything else.

Output ONLY valid JSON:
{"approved": true, "feedback": "Addresses the issue; no stubs"}
or
{"approved": false, "feedback": "File X is referenced in the plan but
the diff doesn't touch it. Also main.go:42 has a TODO stub."}
```

The narrow check list is deliberate: compile/vet already handle hallucinated symbols; the LLM's job is only what code execution can't verify. This keeps the review cheap and the false-positive rate low.

**Model and cost:**

- Model: `gemini-2.5-flash` (already priced in `internal/cost/pricing.go:20`)
- Expected tokens per call: ~8k input, ~150 output → ~$0.0013 per review
- Token counts and calculated cost are captured from the response's usage metadata and stored on `Verdict.InputTokens`, `Verdict.OutputTokens`, `Verdict.CostUSD`.

**JSON parsing:** same as `planner.ReviewPlan` — `ResponseMIMEType: "application/json"` on the request, then `llmutil.CleanJSON` + `json.Unmarshal` into a small internal struct, then populate the `Verdict`.

**LLM failure handling:** if the Gemini call errors (network, quota, malformed JSON after `CleanJSON`), `Review` returns `(nil, err)`. The caller treats this as a gate failure, not a rejection — log and halt. A transient Gemini outage must not silently let hallucinated code through. Matches how `planner.ReviewPlan` errors are handled today (`log.Fatalf` at `cmd/implementer/main.go:153`).

### Short-circuit behavior

```
build fails       → category="build",    feedback=build output,        skip vet + LLM
build ok, vet fail → category="vet",      feedback=vet output,          skip LLM
build ok, vet ok   → run semantic LLM check → category="" or "semantic"
```

Rationale: if the code doesn't compile, asking Gemini "does this address the issue?" produces noise. Better to feed the compile error directly to the implementer on retry.

## Caller orchestration

`cmd/implementer/main.go` adds a new step 9a block. Sketch (illustrative, not literal):

```go
// 9. (existing) git diff / git status …

// 9a. Internal code review (NEW)
log.Printf("Running internal code reviewer...")
verdict, err := codereviewer.Review(ctx, geminiKey, repoDir, fullIssue, plan, dossier)
if err != nil {
    log.Fatalf("code reviewer failed: %v", err)
}
log.Printf("Code review verdict: approved=%v category=%q summary=%s",
    verdict.Approved, verdict.Category, verdict.Summary)

if !verdict.Approved {
    log.Printf("Code review rejected: %s", verdict.Feedback)
    log.Printf("Retrying implementer with reviewer feedback...")

    retryPlan := &planner.ImplementationPlan{
        Markdown: plan.Markdown +
            "\n\n## Reviewer Feedback\n\nThe previous implementation was rejected:\n\n" +
            verdict.Feedback +
            "\n\nFix the issues above. The build and vet checks are re-run after your changes.",
    }

    retryResult, err := implementer.RunAgent(ctx, anthropicKey, modelName,
        repoDir, retryPlan, maxIter, implMaxCost)
    if err != nil {
        log.Fatalf("implementer retry failed: %v", err)
    }

    // Merge retry tokens into the primary Result so run-summary
    // reflects total implementer cost.
    result.Iterations += retryResult.Iterations
    result.InputTokens += retryResult.InputTokens
    result.OutputTokens += retryResult.OutputTokens
    result.CacheCreationTokens += retryResult.CacheCreationTokens
    result.CacheReadTokens += retryResult.CacheReadTokens
    if retryResult.BudgetExceeded {
        result.BudgetExceeded = true
    }

    if result.BudgetExceeded {
        log.Printf("Implementer budget exceeded during retry — halting")
        writeCodeReviewArtifact(artifactDir, verdict, /* retried */ true)
        os.Exit(1)
    }

    // Re-review.
    verdict, err = codereviewer.Review(ctx, geminiKey, repoDir, fullIssue, plan, dossier)
    if err != nil {
        log.Fatalf("code reviewer (retry) failed: %v", err)
    }
    if !verdict.Approved {
        log.Printf("Code review still rejected after retry: %s", verdict.Feedback)
        writeCodeReviewArtifact(artifactDir, verdict, /* retried */ true)
        log.Fatalf("halting before PR creation — code review failed twice")
    }
    log.Printf("Code review approved after retry")
}

writeCodeReviewArtifact(artifactDir, verdict, /* retried */ false)

// 10. (existing) UpsertBranchAndPR …
```

**Design notes embedded here:**

1. **The retry feeds the *original* plan, not a new plan.** We deliberately don't re-run the planner. The plan was already approved by `planner.ReviewPlan`; the failure is in implementation fidelity. Re-running planner would risk changing targets mid-flight.
2. **The retry keeps the same `repoDir`.** No `git reset --hard` between attempts — the retry should build on any correct changes the first run made. If the implementer leaves bad state, the re-review's `go build` will catch it.
3. **`log.Fatalf` does not run deferred functions.** We explicitly call `writeCodeReviewArtifact` *before* every `Fatalf` to ensure the verdict lands in `run-summary.json`.

## Artifact & dashboard integration

### `run-summary.json` extension

The existing `writeRunArtifacts` at `cmd/implementer/main.go:470-505` gains a new top-level `code_review` field, written via a new helper `writeCodeReviewArtifact(dir, verdict, retried bool)`:

```json
{
  "issue_number": 33,
  "iterations": 7,
  "estimated_cost_usd": 0.0621,
  "code_review": {
    "approved": true,
    "category": "",
    "summary": "Addresses issue; no stubs",
    "retried": false,
    "build_passed": true,
    "vet_passed": true,
    "semantic_result": "Addresses issue; no stubs",
    "input_tokens": 7842,
    "output_tokens": 138,
    "cost_usd": 0.00126
  }
}
```

On rejection, `approved: false`, the relevant `category`, the build/vet output or semantic feedback in `summary`, and `retried: true` if the retry was consumed.

`writeCodeReviewArtifact` mirrors the `appendPRURL` pattern at `cmd/implementer/main.go:508-526` — read JSON, merge field, write back. No-op when `artifactDir` is empty.

### Cost accounting

- Top-level `estimated_cost_usd` continues to reflect **implementer cost only** (what `IMPL_MAX_COST` caps). Budget semantics stay stable.
- Reviewer cost lives under `code_review.cost_usd`. Different model, different budget story, separate dashboard series.

### Dashboard

The aggregation workflow (`aggregate-dashboard.yml`) will pick up the new `code_review` block automatically once it lands in `run-summary.json`. Surfacing it visually in `docs/dashboard/index.html` is out of scope for this spec — it's a follow-up. The artifact shape is designed so the dashboard can start reading `code_review.approved`, `code_review.category`, and `code_review.cost_usd` whenever someone is ready.

### Logging

At pipeline level the new step produces these log lines, mirroring the existing plan-reviewer output:

```
Running internal code reviewer...
Code review verdict: approved=true category="" summary="..."
[or on rejection]
Code review rejected: <feedback>
Retrying implementer with reviewer feedback...
Retry completed in N iterations
Code review approved after retry
[or]
Code review still rejected after retry: <feedback>
```

These land in the CI job log and get captured by the existing artifact upload.

## Testing

### Unit tests — `internal/codereviewer/reviewer_test.go`

1. **`TestRunBuild_Passes`** — temp dir with trivial valid Go module, assert `Passed=true`.
2. **`TestRunBuild_Fails`** — same setup but `main.go` references an undefined symbol, assert `Passed=false` and output contains "undefined".
3. **`TestRunVet_CatchesShadow`** — valid build but vet-flagged code (e.g. `fmt.Printf` with wrong format verb), assert `Passed=false`.
4. **`TestRunBuild_Timeout`** — 1 ns deadline context, assert the function returns an error (not a verdict).
5. **`TestRunBuild_OutputTruncation`** — pathologically long error, assert output is truncated to 16 KiB.
6. **`TestBuildReviewPrompt`** — smoke test mirroring `TestBuildReviewerPrompt` at `internal/planner/planner_test.go:72-83`. Assert the prompt contains the issue title, plan markdown, and diff.
7. **`TestReview_ShortCircuitsOnBuildFailure`** — call `Review` with `geminiKey=""` against a repo that fails to build. Assert the LLM is never called (empty key would fail it), verdict is `{approved: false, category: "build"}`, no error returned. Proves short-circuit without a mock.
8. **`TestReview_ShortCircuitsOnVetFailure`** — same idea for vet.

The Gemini SDK itself is not mocked. Neither `planner.ReviewPlan` nor `archivist` mock the client today, and introducing a fake just for this would be disproportionate. Precedent: test the prompt builder, trust the SDK, validate end-to-end with the integration test.

### Integration test — skipped in short mode

`TestReview_Integration` in the same file, guarded by `testing.Short()` and `GOOGLE_API_KEY` presence:

```go
func TestReview_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    geminiKey := os.Getenv("GOOGLE_API_KEY")
    if geminiKey == "" {
        t.Skip("GOOGLE_API_KEY not set")
    }
    // Clone a small known-good repo to temp, introduce a deliberately
    // broken change, call Review, assert rejection.
}
```

Runs live against Gemini Flash when keys are present. It's the only real signal that end-to-end wiring works; unit tests alone can't prove that. The test is skipped under `-short` and when no key is set, so it won't break local `go test ./...` runs or CI where the key isn't plumbed; it's available for manual verification via `go test ./internal/codereviewer -run Integration`.

### No `cmd/implementer` tests

`main.go` has no existing test file and adding one just for the new orchestration block is disproportionate. The logic there is intentionally thin — call `Review`, branch on verdict, merge token counts. The integration test in `reviewer_test.go` plus existing CI pipeline runs provide coverage.

### TDD order

1. Tests 1–5 (deterministic check suite) red → implement `checks.go` → green.
2. Test 6 (prompt builder) red → implement prompt builder → green.
3. Implement `Review` short-circuit logic.
4. Tests 7–8 red → wire short-circuit into `Review` → green.
5. Implement LLM call and `Verdict` population.
6. Integration test last, gated behind `-short` and `GOOGLE_API_KEY`.

## Out of scope

- Running target-repo tests (`go test`) as part of the verdict — deferred; tests are CI's job, and Conduit tests are known-flaky in the agent environment.
- Running target-repo linters (`golangci-lint`, etc.) — that's issue #34, a separate ticket. This spec is semantic review; #34 is syntactic/style.
- Dashboard UI surfacing of the `code_review` block — follow-up, artifact shape is designed to support it.
- A `CODEREVIEW_ENABLED` kill switch — not adding one. If disabling is ever needed, a small revert PR is clearer than drift-prone env var guards.
- Cost cap separate from `IMPL_MAX_COST` — not adding one. The reviewer's Gemini call is ~$0.0013; it doesn't warrant a separate budget.
- Multiple retries — capped at one, full stop. More retries invite runaway cost and rarely help.

## References

- Issue #33 — Add internal code review step after implementer (pre-PR)
- Related #34 — Run target repo's linter before creating PR (complementary, syntactic)
- Existing plan reviewer — `internal/planner/reviewer.go`
- Existing run-artifact helpers — `cmd/implementer/main.go:470-526`
- Existing budget cap — `internal/cost/budget.go`, `docs/cost-analysis.md`
