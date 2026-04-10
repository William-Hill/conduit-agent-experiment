# Target-repo linter as a pre-PR gate

**Issue:** #34
**Date:** 2026-04-10
**Status:** Approved

## Problem

PRs opened by the pipeline are failing CI lint checks on ConduitIO/conduit. The implementer runs `go build` and `go vet` via its internal tool loop and PR #36 added a pre-push code reviewer that catches semantic issues, but nothing runs the target repo's declared linter rules. As a result, PRs are effectively dead-on-arrival: they fail CI immediately and the responder bot loop wastes iterations fixing lint errors that should have been caught before push.

This spec adds a target-repo linter as a new deterministic check inside the existing pre-push verification gate.

## Goals

- Run the target repo's linter against agent-generated changes before the PR is pushed.
- When lint fails with errors the agent caused, feed structured feedback back through the existing one-retry loop.
- Never block the pipeline on lint errors the agent did not cause (pre-existing debt in the target repo).
- Zero incremental LLM cost; zero new retry slots; zero changes to the implementer's retry block.

## Non-goals

- Non-Go language support (Python, ruff, ESLint, Clippy). Go-first v1; the dispatch slot leaves room for later additions.
- Auto-installation of `golangci-lint` at runtime. Operators install the binary into the runner image or the target repo's `make lint` target handles it.
- Dashboard changes for lint metrics. The artifact gains new fields; the dashboard can pick them up in a follow-up.
- Changes to `cmd/implementer/main.go`'s retry block. The existing unified one-retry loop is reused unchanged.
- A new `internal/linter` package. Lint is a natural addition to the existing `internal/codereviewer` package.

## Architecture

Extend `internal/codereviewer` with a new deterministic check, `RunLint`, that runs between `go vet` and the semantic Gemini call inside the existing `Review()` function.

Flow inside `Review()`:

1. `RunBuild` — unchanged
2. `RunVet` — unchanged
3. **`collectChangedFiles` — new** (hoisted from the old `collectDiff` so both lint and the semantic reviewer see the same changed-file set)
4. **`RunLint` — new**
5. `collectDiff` — refactored to diff-only; no longer returns the changed-file list
6. Gemini semantic review — unchanged

If `RunLint` reports **actionable** errors (see *Feedback filtering* below), `Review()` returns `verdict.Approved = false` with `verdict.Category = "lint"` and the retry block at `cmd/implementer/main.go:240` handles the rest of the lifecycle unchanged. One retry covers lint and semantic review together — no new retry slots, no new budget arithmetic.

### Why in-package, not a new `internal/linter` package

The original issue sketch proposed a new package. Colocating with `codereviewer` wins because:

- Lint and code review are both pre-push verification gates. Splitting them creates two nearly-identical retry/telemetry/artifact code paths.
- One retry budget for all pre-push gates keeps the cost contract simple. A separate package would invite a separate retry slot.
- `runGo` already establishes the bounded-env / capped-output pattern lint needs. Reusing it avoids duplication.
- The "distinct from #33" language in the issue is about semantic-vs-syntactic *concerns*, not physical package boundaries.

## Linter detection and invocation

New file `internal/codereviewer/linter.go`. New unexported type:

```go
type lintConfig struct {
    Mode       string // "make" or "golangci-lint"
    ConfigPath string // detected config file (empty for make mode)
}
```

### Detection

`detectLinter(repoDir string) (*lintConfig, error)` probes in this order:

1. **Make target probe.** If `<repoDir>/Makefile` exists and a `^lint:` target is present (regex match against file contents, bounded to the first 64 KiB read), return `{Mode: "make"}`. This is the primary path against ConduitIO/conduit.
2. **golangci-lint probe.** Otherwise, check `exec.LookPath("golangci-lint")`. If the binary is present **and** a config file exists at one of `.golangci.yml`, `.golangci.yaml`, or `.golangci.toml`, return `{Mode: "golangci-lint", ConfigPath: <found>}`.
3. **No-op.** Otherwise return `nil, nil`. `RunLint` turns this into `CheckResult{Passed: true, Output: "lint: no configuration detected, skipped"}` and logs a warning. The pipeline proceeds.

### Invocation

`RunLint(ctx context.Context, repoDir string) (*CheckResult, error)`:

- Reuses `checkTimeout` (2 min) and the minimal-env strategy from `runGo`: `GOFLAGS=-mod=readonly`, `GOWORK=off`, and only `PATH`, `HOME`, `GOPATH`, `GOROOT`, `TMPDIR` from the host environment. Identical security posture to build/vet.
- **Make mode:** `make lint` with working dir `repoDir`. Combined stdout+stderr captured via `cappedBuffer` at `maxCheckOutput` (16 KiB), same truncation semantics as `runGo`.
- **golangci-lint mode:** `golangci-lint run --out-format=line-number ./...`. The `line-number` format is deterministic `file:line:col: message (linter)` which the feedback parser handles reliably. Same capture/truncation.
- Returns `CheckResult{Passed: exitCode == 0, ExitCode: ..., Output: ...}`.

### Escape hatch

Environment variable `AGENT_LINT=off` short-circuits detection to return `nil, nil`. This is the operator override when a target repo's lint target is broken on `main` or during incidents. Defaults to on.

## Feedback filtering

Pre-existing lint debt in a target repo must not cause us to fail retries for errors we didn't introduce. After `RunLint` returns a failure, filter the output to errors in changed files before deciding whether the failure is actionable.

New helper:

```go
type lintError struct {
    File    string
    Line    int
    Col     int
    Message string
}

// filterLintErrors parses lint output and returns only errors whose
// file path falls within changedFiles. The second return is the count
// of parsed-but-dropped errors (for telemetry/logging). The third
// return is true when parsing stopped at lintParseCap — callers must
// treat a truncated parse with no kept errors as UNSAFE for advisory
// pass, because changed-file errors may exist beyond the cap.
//
// repoDir lets the parser strip an absolute-path prefix before the
// changedFiles lookup, so a linter invoked with `$(PWD)` in a Makefile
// still matches a relative changedFiles entry. Pass "" to only trim a
// leading "./".
func filterLintErrors(output, repoDir string, changedFiles []string) (kept []lintError, dropped int, truncated bool)
```

### Parser

Line-oriented regex: `^(?P<file>[^:]+):(?P<line>\d+):(?:(?P<col>\d+):)?\s*(?P<msg>.+)$`. It matches both `golangci-lint`'s `line-number` format and the common `file:line:col: message` format most Go linters emit (including anything `make lint` is likely to wrap). Non-matching lines are ignored — they were never parsed as errors, so they aren't counted as dropped. Hard cap: stop after parsing 500 error lines to bound cost on pathological output; the caller is expected to surface the cap hit via the `truncated` return and fail closed when it fires with zero kept errors.

### Path matching

`changedFiles` comes from the existing `collectDiff` (`git add -N .` + `git status --porcelain`). Refactor: hoist the changed-files collection out of `collectDiff` into a new helper `collectChangedFiles(ctx, repoDir) ([]string, error)` and call it **before** `RunLint` so the changed-file set is available for filtering. `collectDiff` is refactored to call the new helper internally so there's a single source of truth — no duplicated `git status --porcelain` code. The diff collection (`git diff HEAD`) itself stays inside `collectDiff` because only the semantic review needs it. A parsed error matches if its `File`, after stripping a leading `./` and any `repoDir` prefix, equals any entry in `changedFiles`.

### Decision logic inside `Review()`

```go
if lintResult.Passed {
    verdict.LintOutput = lintResult.Output
    // proceed to semantic review
} else {
    kept, dropped, parserTruncated := filterLintErrors(lintResult.Output, repoDir, changedFiles)
    verdict.LintOutput = lintResult.Output
    verdict.LintErrorsKept = len(kept)
    verdict.LintErrorsDropped = dropped

    if len(kept) > 0 {
        // Real agent-introduced errors — reject with formatted feedback.
        verdict.Approved = false
        verdict.Category = "lint"
        verdict.Summary = fmt.Sprintf("%d lint error(s) in changed files", len(kept))
        verdict.Feedback = formatLintFeedback(kept)
        return verdict
    }

    // Fail closed when we cannot trust the advisory-pass conclusion:
    //   - dropped == 0 means zero parseable lines (unparseable format,
    //     Makefile tooling error, ANSI codes, unknown linter output)
    //   - parserTruncated means the parser stopped at lintParseCap and
    //     we may have missed changed-file errors past the cap
    //   - the "... (truncated)" sentinel means runBoundedCmd capped
    //     the raw output at 16 KiB and we may have missed errors past
    //     the cap
    outputTruncated := strings.Contains(lintResult.Output, "... (truncated)")
    if dropped == 0 || parserTruncated || outputTruncated {
        verdict.Approved = false
        verdict.Category = "lint"
        verdict.Summary = "lint failed with unclassifiable output"
        verdict.Feedback = formatLintRawFeedback(lintResult.Output)
        return verdict
    }

    // Safe advisory pass — parser read the full output, at least one
    // error was parsed, and none of them touched changed files.
    log.Printf("Lint advisory pass: %d error(s) reported but all in unchanged files (pre-existing debt)", dropped)
    // proceed to semantic review
}
```

### Feedback format

`formatLintFeedback(errs []lintError) string` returns:

```markdown
## Lint Errors

The following lint violations were introduced by your changes. Fix each one:

- internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)
- internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)
- cmd/implementer/main.go:301:12: exported function Frob should have comment (golint)

Re-run the build and try again.
```

The fail-closed path uses a sibling `formatLintRawFeedback(output string) string` that wraps the raw lint output in a `## Lint Failure` block so the implementer can diagnose unparseable/truncated runs directly.

This string flows into `verdict.Feedback`, which the existing retry block at `cmd/implementer/main.go:245` appends to the retry plan with no changes.

## Data model changes

`internal/codereviewer/types.go` gains three new fields on `Verdict`:

```go
LintOutput        string `json:"lint_output,omitempty"`
LintErrorsKept    int    `json:"lint_errors_kept"`
LintErrorsDropped int    `json:"lint_errors_dropped"`
```

`cmd/implementer/main.go`'s `writeCodeReviewArtifact` currently builds a hand-curated `map[string]any` that is merged into `run-summary.json` under a `code_review` key (there is no separate `code-review.json`). The fix for #34 extends that map to include `lint_output`, `lint_errors_kept`, and `lint_errors_dropped` alongside the existing diagnostic fields, and also extends the `buildPassed` / `vetPassed` derivation to recognize `Category="lint"` as "both earlier gates passed."

## Logging

`Review()` gains two new log lines, mirroring the existing `go build` / `go vet` patterns:

- `log.Printf("Running linter (mode=%s)...", cfg.Mode)` before invocation.
- `log.Printf("Lint check: passed=%v kept=%d dropped=%d", passed, kept, dropped)` after the decision.

## Testing

### Unit tests — new file `internal/codereviewer/linter_test.go`

**`detectLinter`** (table-driven, `t.TempDir()`-backed fake repos):

- Makefile with `^lint:` target → `Mode: "make"`.
- Makefile without a lint target → falls through to golangci-lint probe.
- No Makefile, `.golangci.yml` present, `golangci-lint` on PATH → `Mode: "golangci-lint"` (stub `lookPathFn` package var to avoid host-environment dependency).
- No Makefile, `.golangci.yml` present, no binary → `nil, nil`.
- Empty repo → `nil, nil`.
- `AGENT_LINT=off` → `nil, nil` regardless of other state.
- Makefile > 64 KiB → only first 64 KiB scanned (bounded read).

**`filterLintErrors`** (pure function, pure table test):

- Canonical `file:line:col: message (linter)` format with mixed changed/unchanged files → correct `kept` and `dropped` counts.
- `file:line: message` (no column) → parsed correctly.
- Path normalization: `./internal/foo.go` matches `internal/foo.go` in `changedFiles`.
- Non-matching garbage lines → ignored (not counted as errors).
- 1000 error lines → capped at 500 parsed.
- Empty output → `nil, 0`.

**`formatLintFeedback`** — golden string test, single case with three errors.

### Integration tests — extend `reviewer_test.go`

Add a package var `runLintFn = RunLint` so tests can stub it. New cases:

1. **Lint passes → semantic review runs.** Stub `reviewSemantics` to approve. Verdict approved; `verdict.LintOutput` populated.
2. **Lint fails with errors in changed files → verdict rejected.** `Category: "lint"`. Semantic review is *not* called (assert stub call count == 0). Feedback matches `formatLintFeedback`.
3. **Lint fails with errors only in unchanged files → advisory pass.** Semantic review runs; verdict approved. Assert `verdict.LintErrorsDropped > 0` and `verdict.LintErrorsKept == 0`.
4. **Lint detection returns nil → lint silently skipped.** Semantic review runs normally. Verdict approved. `verdict.LintOutput` contains the "no configuration detected" sentinel.

### Smoke test (manual, pre-merge)

1. Clone ConduitIO/conduit locally. Run the pipeline end-to-end against a known issue with the new linter gate enabled. Verify `make lint` runs, verify output flows through to the artifact.
2. Seed a deliberate lint error (e.g., unused variable in a changed file). Verify the gate catches it, the retry fires with the expected feedback, and either the retry fixes it or the pipeline halts without pushing.
3. Verify `AGENT_LINT=off` short-circuits correctly.

Real `make` and real `golangci-lint` execution are intentionally not exercised in unit tests — matching the existing contract for `RunBuild` / `RunVet`, which stub `runGo` in tests and rely on real runs during development.

## Cost

Zero LLM cost. Lint is local-only. Incremental cost:

- Wall time: up to `checkTimeout` (2 min) per run against ConduitIO/conduit. Typical `make lint` on conduit is likely 30–60s.
- Possible extra implementer retry (~$0.15 at Haiku rates) if lint catches something the build/vet gates didn't. Bounded by the existing `IMPL_MAX_COST` cap via the shared retry slot — we cannot spend more than the existing ceiling.

No new cost-pricing entries.

## Acceptance criteria mapping to issue #34

| Issue AC | Covered by |
|---|---|
| Linter config auto-detected in cloned target repo (Go first) | `detectLinter` |
| Linter installed/invoked per detected config | Invocation via `make lint` or `golangci-lint run`; install explicitly out-of-scope |
| Linter run against changed files only | Filtered feedback via `filterLintErrors` (filtered feedback, not filtered invocation) |
| Lint errors parsed into structured format | `lintError` + parser regex |
| Errors fed back to implementer for one retry | Shared retry slot via existing `cmd/implementer/main.go:240` |
| Pipeline halts with no PR if lint fails after retry | Existing retry block at `cmd/implementer/main.go:343` |
| Tested against ConduitIO/conduit with intentionally sloppy code | Smoke test step 2 |
| Tested against repo without lint config (no-op fallback) | Unit test for `detectLinter` empty-repo case |
| Cost tracker records the step | No change — zero cost |
| Dashboard shows lint pass/fail rate per run | **Deferred** to a follow-up PR (artifact gains the fields; dashboard consumes them later) |

## Open questions

None at spec time.

## References

- Issue: conduit-agent-experiment#34
- Related: #33 / PR #36 — internal code reviewer (the pattern this extends)
- Code: `internal/codereviewer/reviewer.go`, `internal/codereviewer/checks.go`, `cmd/implementer/main.go:200-352`
