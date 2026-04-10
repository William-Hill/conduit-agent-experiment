# Internal Code Reviewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship issue #33 — a post-implementer, pre-push gate that re-runs `go build`/`go vet` externally and makes one Gemini Flash call for semantic checks, with one-retry feedback to the implementer and halt-on-second-rejection.

**Architecture:** New `internal/codereviewer/` package exposes a single `Review(ctx, geminiKey, repoDir, issue, plan, dossier)` entrypoint. Internals are split into `types.go` (result types), `checks.go` (deterministic `go build`/`go vet` runners with bounded env and 16 KiB output cap), and `reviewer.go` (diff collection, prompt builder, Gemini Flash call, short-circuit orchestration). Caller (`cmd/implementer/main.go`) owns retry, token accounting, artifact writes, and exit codes as a new step 9a between the existing change-detection step and the PR upsert.

**Tech Stack:** Go 1.21+, `google.golang.org/genai` v1.40.0 (already in go.mod), `github.com/mjhilldigital/conduit-agent-experiment/internal/{archivist,cost,github,llmutil,planner}` (existing internal packages), `os/exec` + `git` CLI for diff collection.

**Spec:** `docs/specs/2026-04-09-internal-code-reviewer-design.md`

**Pre-work for the executor:**
- Create a worktree for this sprint using the `superpowers:using-git-worktrees` skill before starting Task 1. Branch name suggestion: `feat/codereviewer`.
- Confirm `GOOGLE_API_KEY` is available in the shell env; not strictly needed until Task 8, but handy to verify early.

**File structure (all deltas):**
- `internal/codereviewer/types.go` — NEW — `Verdict`, `CheckResult` types (no external deps)
- `internal/codereviewer/checks.go` — NEW — `RunBuild`, `RunVet` + shared `runGo` helper
- `internal/codereviewer/reviewer.go` — NEW — `Review` entrypoint, `collectDiff`, `buildReviewPrompt`, `callGemini`
- `internal/codereviewer/reviewer_test.go` — NEW — all unit tests + integration test
- `cmd/implementer/main.go` — MODIFY — add `writeCodeReviewArtifact` helper and new step 9a block between existing step 9 (L197-221) and step 10 (L223-258)

---

## Task 1: Package scaffold and types

**Files:**
- Create: `internal/codereviewer/types.go`

- [ ] **Step 1: Create `internal/codereviewer/types.go`**

```go
// Package codereviewer runs a post-implementer, pre-push verification
// gate: re-runs `go build`/`go vet` against the working tree, then
// makes a single Gemini Flash call to catch semantic problems the
// compiler can't see (stubs, unfinished code, unrelated changes).
//
// Motivation: the implementer's internal build check is not reliable —
// hallucinated symbols and stubs have made it all the way to published
// PRs. This package is an external verification layer that does not
// trust the agent's self-report.
package codereviewer

// Verdict is the final outcome of a code review.
type Verdict struct {
	Approved bool `json:"approved"`
	// Category indicates which gate failed, if any.
	// One of: "build", "vet", "semantic", "".
	Category string `json:"category,omitempty"`
	// Summary is a human-readable one-liner for logs and dashboards.
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
	// Output is combined stdout+stderr, truncated to 16 KiB.
	Output string
}
```

- [ ] **Step 2: Verify the file compiles**

Run: `go build ./internal/codereviewer/...`
Expected: success (no tests yet; just a type-only package).

- [ ] **Step 3: Commit**

```bash
git add internal/codereviewer/types.go
git commit -m "feat(codereviewer): add Verdict and CheckResult types (#33)"
```

---

## Task 2: `RunBuild` with pass and fail tests

**Files:**
- Create: `internal/codereviewer/checks.go`
- Create: `internal/codereviewer/reviewer_test.go`

- [ ] **Step 1: Write the failing tests (and shared test helpers)**

Create `internal/codereviewer/reviewer_test.go` with:

```go
package codereviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestModule creates a temp dir with go.mod and main.go, returning
// the directory path. Uses t.TempDir so cleanup is automatic.
func writeTestModule(t *testing.T, mainContent string) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "main.go"), mainContent)
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestRunBuild_Passes(t *testing.T) {
	dir := writeTestModule(t, "package main\n\nfunc main() {}\n")
	res, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true, got false. Output: %s", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", res.ExitCode)
	}
}

func TestRunBuild_Fails(t *testing.T) {
	// undefinedSymbol() is an unresolved reference → compile error.
	dir := writeTestModule(t, "package main\n\nfunc main() { undefinedSymbol() }\n")
	res, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if res.Passed {
		t.Error("expected Passed=false for undefined symbol")
	}
	if res.ExitCode == 0 {
		t.Error("expected non-zero ExitCode")
	}
	if !strings.Contains(res.Output, "undefined") {
		t.Errorf("expected output to contain 'undefined', got: %s", res.Output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/codereviewer/ -run TestRunBuild -v`
Expected: FAIL with "undefined: RunBuild" (package doesn't export it yet).

- [ ] **Step 3: Implement minimal `checks.go`**

Create `internal/codereviewer/checks.go`:

```go
package codereviewer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// checkTimeout is the hard cap for a single deterministic check.
// Matches internal/implementer/tools.go:268 (run_command tool).
const checkTimeout = 2 * time.Minute

// maxCheckOutput caps combined stdout+stderr at 16 KiB so pathological
// build logs can't blow up the LLM prompt or run-summary artifact.
const maxCheckOutput = 16 * 1024

// runGo executes `go <sub> ./...` in repoDir with a bounded environment
// and timeout. The minimal env mirrors internal/implementer/tools.go:275-281
// to prevent a compromised target repo from exfiltrating API keys.
func runGo(ctx context.Context, repoDir, sub string) (*CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", sub, "./...")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"GOROOT=" + os.Getenv("GOROOT"),
		"TMPDIR=" + os.TempDir(),
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Deadline exceeded is a runner error, not a verdict.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("go %s ./... timed out after %s", sub, checkTimeout)
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., go binary missing) — bubble up.
			return nil, fmt.Errorf("running go %s: %w", sub, runErr)
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	if len(output) > maxCheckOutput {
		output = output[:maxCheckOutput] + "\n... (truncated)"
	}

	return &CheckResult{
		Passed:   exitCode == 0,
		ExitCode: exitCode,
		Output:   output,
	}, nil
}

// RunBuild executes `go build ./...` in repoDir.
func RunBuild(ctx context.Context, repoDir string) (*CheckResult, error) {
	return runGo(ctx, repoDir, "build")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/codereviewer/ -run TestRunBuild -v`
Expected: PASS for both `TestRunBuild_Passes` and `TestRunBuild_Fails`.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/checks.go internal/codereviewer/reviewer_test.go
git commit -m "feat(codereviewer): add RunBuild with pass/fail tests (#33)"
```

---

## Task 3: `RunVet`

**Files:**
- Modify: `internal/codereviewer/checks.go`
- Modify: `internal/codereviewer/reviewer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/codereviewer/reviewer_test.go`:

```go
func TestRunVet_CatchesFormatMismatch(t *testing.T) {
	// fmt.Printf with wrong format verb — go build passes, go vet fails.
	content := `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`
	dir := writeTestModule(t, content)

	// Sanity: build should pass (vet is what catches this).
	build, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if !build.Passed {
		t.Fatalf("expected build to pass, got: %s", build.Output)
	}

	vet, err := RunVet(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunVet error: %v", err)
	}
	if vet.Passed {
		t.Errorf("expected vet to fail on %%d vs string mismatch. Output: %s", vet.Output)
	}
	if vet.ExitCode == 0 {
		t.Errorf("expected non-zero ExitCode")
	}
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/codereviewer/ -run TestRunVet -v`
Expected: FAIL with "undefined: RunVet".

- [ ] **Step 3: Add `RunVet` to `checks.go`**

Append to `internal/codereviewer/checks.go`:

```go
// RunVet executes `go vet ./...` in repoDir.
func RunVet(ctx context.Context, repoDir string) (*CheckResult, error) {
	return runGo(ctx, repoDir, "vet")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codereviewer/ -run TestRunVet -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/checks.go internal/codereviewer/reviewer_test.go
git commit -m "feat(codereviewer): add RunVet with format-mismatch test (#33)"
```

---

## Task 4: Timeout and output truncation

**Files:**
- Modify: `internal/codereviewer/reviewer_test.go`

- [ ] **Step 1: Write the failing tests**

Add these imports to the existing import block at the top of `reviewer_test.go` (keep existing imports too):

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)
```

Append these two tests:

```go
func TestRunBuild_Timeout(t *testing.T) {
	dir := writeTestModule(t, "package main\n\nfunc main() {}\n")
	// Deadline in the past → exec will be cancelled before it can run.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // ensure deadline has elapsed

	_, err := RunBuild(ctx, dir)
	if err == nil {
		t.Error("expected error from expired context, got nil")
	}
}

func TestRunBuild_OutputTruncation(t *testing.T) {
	// Produce enough compile errors to exceed the 16 KiB cap.
	// Each "undefined: noSuchSymbolN" line is ~60 chars; 500 → ~30 KiB.
	var sb strings.Builder
	sb.WriteString("package main\n\nfunc main() {\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&sb, "\tnoSuchSymbol%d()\n", i)
	}
	sb.WriteString("}\n")

	dir := writeTestModule(t, sb.String())
	res, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected build to fail")
	}
	// Allow small overhead for the "... (truncated)" suffix.
	if len(res.Output) > maxCheckOutput+64 {
		t.Errorf("output length %d exceeds cap %d", len(res.Output), maxCheckOutput+64)
	}
	if !strings.Contains(res.Output, "truncated") {
		t.Error("expected output to contain truncation marker")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/codereviewer/ -v`
Expected: PASS for both new tests. `TestRunBuild_Timeout` passes because `exec.CommandContext` observes the expired deadline and returns an error. `TestRunBuild_OutputTruncation` passes because the existing `runGo` helper already truncates at `maxCheckOutput`.

If `TestRunBuild_OutputTruncation` fails because go emits fewer than 16 KiB of errors (go's error reporter may cap), double the loop count to 1000 and rerun. If it still fails, add one more file with more errors — diagnose, don't paper over.

- [ ] **Step 3: Commit**

```bash
git add internal/codereviewer/reviewer_test.go
git commit -m "test(codereviewer): add timeout and output truncation tests (#33)"
```

---

## Task 5: Prompt builder

**Files:**
- Create: `internal/codereviewer/reviewer.go`
- Modify: `internal/codereviewer/reviewer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `reviewer_test.go`:

```go
func TestBuildReviewPrompt(t *testing.T) {
	issue := &github.Issue{
		Number: 42,
		Title:  "Fix the bug",
		Body:   "The thing is broken",
	}
	plan := &planner.ImplementationPlan{
		Markdown: "# Plan\n\nWrite code to fix main.go",
	}
	dossier := &archivist.Dossier{
		Summary:  "Bug is in main.go",
		Approach: "Fix the nil check",
		Files: []archivist.FileEntry{
			{Path: "main.go", Reason: "has the bug"},
		},
	}
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	files := []string{"main.go"}

	prompt := buildReviewPrompt(issue, plan, dossier, diff, files)

	for _, want := range []string{
		"Fix the bug",
		"The thing is broken",
		"Write code to fix main.go",
		"Bug is in main.go",
		"Fix the nil check",
		"main.go",
		"+new",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt should contain %q", want)
		}
	}
}
```

And add these imports to the top import block of `reviewer_test.go`:

```go
	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
```

The final import block should read:

```go
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/codereviewer/ -run TestBuildReviewPrompt -v`
Expected: FAIL with "undefined: buildReviewPrompt".

- [ ] **Step 3: Create `reviewer.go` with only the prompt builder**

Create `internal/codereviewer/reviewer.go`:

```go
package codereviewer

import (
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

const codeReviewSystemPrompt = `You are a code review engineer. You receive a GitHub issue, a plan, a research dossier, and a git diff of an attempted implementation. The code already compiles (go build) and passes go vet.

Check ONLY these semantic concerns:
1. Does the diff actually address the issue in the plan?
2. Are there obvious stubs, TODO/FIXME markers, or unfinished code ("... rest of implementation" etc.)?
3. Are there changes to files unrelated to the plan?
4. Does the diff drop or ignore requirements the plan explicitly called out?

Do NOT flag style, naming, test coverage, or "could be cleaner" concerns — those are for CI and human review. Be strict about stubs and missing work; lenient about everything else.

Output ONLY valid JSON:
{"approved": true, "feedback": "Addresses the issue; no stubs"}
or
{"approved": false, "feedback": "File X is referenced in the plan but the diff doesn't touch it. Also main.go:42 has a TODO stub."}`

// buildReviewPrompt assembles the user-side prompt passed to Gemini
// alongside codeReviewSystemPrompt. Order: issue, plan, dossier,
// touched files, diff (diff last because it's the largest section).
func buildReviewPrompt(issue *github.Issue, plan *planner.ImplementationPlan, dossier *archivist.Dossier, diff string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Issue #%d: %s\n\n%s\n\n", issue.Number, issue.Title, issue.Body)
	fmt.Fprintf(&b, "## Plan\n\n%s\n\n", plan.Markdown)
	fmt.Fprintf(&b, "## Research Summary\n\n%s\n\n", dossier.Summary)
	if dossier.Approach != "" {
		fmt.Fprintf(&b, "## Intended Approach\n\n%s\n\n", dossier.Approach)
	}
	b.WriteString("## Touched Files\n\n")
	if len(files) == 0 {
		b.WriteString("(none detected)\n")
	} else {
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	fmt.Fprintf(&b, "\n## Diff\n\n```diff\n%s\n```\n", diff)
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codereviewer/ -run TestBuildReviewPrompt -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/reviewer.go internal/codereviewer/reviewer_test.go
git commit -m "feat(codereviewer): add review prompt builder with test (#33)"
```

---

## Task 6: `Review` with short-circuit logic

**Files:**
- Modify: `internal/codereviewer/reviewer.go`
- Modify: `internal/codereviewer/reviewer_test.go`

This task wires `Review` end-to-end with deterministic short-circuits but stubs the LLM call so tests pass without network. Task 7 replaces the stub with the real Gemini call.

- [ ] **Step 1: Write the failing tests**

Append to `reviewer_test.go`:

```go
func TestReview_ShortCircuitsOnBuildFailure(t *testing.T) {
	// Repo fails to build; empty geminiKey would fail any LLM call —
	// so if short-circuit works, we never reach the LLM and no error bubbles.
	dir := writeTestModule(t, "package main\n\nfunc main() { undefinedSymbol() }\n")
	verdict, err := Review(context.Background(), "", dir,
		&github.Issue{Number: 1, Title: "test", Body: "test"},
		&planner.ImplementationPlan{Markdown: "test plan"},
		&archivist.Dossier{Summary: "test"},
	)
	if err != nil {
		t.Fatalf("Review should not error on build failure (should short-circuit): %v", err)
	}
	if verdict.Approved {
		t.Error("expected Approved=false on build failure")
	}
	if verdict.Category != "build" {
		t.Errorf("expected Category=build, got %q", verdict.Category)
	}
	if verdict.Feedback == "" {
		t.Error("expected non-empty Feedback")
	}
	if !strings.Contains(verdict.BuildOutput, "undefined") {
		t.Errorf("expected BuildOutput to contain 'undefined', got: %s", verdict.BuildOutput)
	}
}

func TestReview_ShortCircuitsOnVetFailure(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`
	dir := writeTestModule(t, content)
	verdict, err := Review(context.Background(), "", dir,
		&github.Issue{Number: 1, Title: "test", Body: "test"},
		&planner.ImplementationPlan{Markdown: "test plan"},
		&archivist.Dossier{Summary: "test"},
	)
	if err != nil {
		t.Fatalf("Review should not error on vet failure (should short-circuit): %v", err)
	}
	if verdict.Approved {
		t.Error("expected Approved=false on vet failure")
	}
	if verdict.Category != "vet" {
		t.Errorf("expected Category=vet, got %q", verdict.Category)
	}
	if verdict.VetOutput == "" {
		t.Error("expected non-empty VetOutput")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/codereviewer/ -run TestReview_ShortCircuits -v`
Expected: FAIL with "undefined: Review".

- [ ] **Step 3: Add `collectDiff`, `Review`, and a stubbed LLM helper to `reviewer.go`**

Append the following to `internal/codereviewer/reviewer.go`. Also extend the import block at the top of the file — it should become:

```go
import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)
```

Then append:

```go
// geminiModel is the model used for the semantic review call. Pricing
// is registered in internal/cost/pricing.go:20.
const geminiModel = "gemini-2.5-flash"

// maxDiffBytes caps the diff fed to the LLM so a giant refactor can't
// blow up the prompt. 32 KiB is ~8k tokens — well under Flash's limit.
const maxDiffBytes = 32 * 1024

// llmVerdict is the JSON shape the semantic LLM returns.
type llmVerdict struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}

// reviewSemantics is a package var so tests can replace it with a stub
// if needed. Task 7 initializes it to the real Gemini Flash call. Until
// then it is nil and the happy-path branch of Review errors cleanly;
// the short-circuit tests in this task never reach that branch.
var reviewSemantics func(ctx context.Context, apiKey, prompt string) (*llmVerdict, int, int, error)

// Review runs the deterministic and semantic gates against the current
// working tree in repoDir. It does not mutate repo content (though
// `git add -N .` is used to surface untracked files in the diff — this
// is a no-op on file contents and only touches the git index).
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
) (*Verdict, error) {
	verdict := &Verdict{}

	// 1. go build
	build, err := RunBuild(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("running go build: %w", err)
	}
	verdict.BuildOutput = build.Output
	if !build.Passed {
		verdict.Approved = false
		verdict.Category = "build"
		verdict.Summary = "go build failed"
		verdict.Feedback = "## Build Failure\n\n" + build.Output
		return verdict, nil
	}

	// 2. go vet
	vet, err := RunVet(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("running go vet: %w", err)
	}
	verdict.VetOutput = vet.Output
	if !vet.Passed {
		verdict.Approved = false
		verdict.Category = "vet"
		verdict.Summary = "go vet failed"
		verdict.Feedback = "## Vet Failure\n\n" + vet.Output
		return verdict, nil
	}

	// 3. Collect the diff (including untracked files via `git add -N .`).
	diff, files, err := collectDiff(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting diff: %w", err)
	}

	// 4. Semantic LLM review.
	if reviewSemantics == nil {
		return nil, fmt.Errorf("semantic reviewer not configured (Task 7 not yet applied)")
	}
	prompt := buildReviewPrompt(issue, plan, dossier, diff, files)
	llmResult, inTokens, outTokens, err := reviewSemantics(ctx, geminiKey, prompt)
	if err != nil {
		return nil, fmt.Errorf("semantic reviewer: %w", err)
	}

	verdict.InputTokens = inTokens
	verdict.OutputTokens = outTokens
	verdict.CostUSD = cost.Calculate(geminiModel, inTokens, outTokens)
	verdict.SemanticResult = llmResult.Feedback

	if !llmResult.Approved {
		verdict.Approved = false
		verdict.Category = "semantic"
		verdict.Summary = "semantic review rejected"
		verdict.Feedback = "## Semantic Review Rejection\n\n" + llmResult.Feedback
		return verdict, nil
	}

	verdict.Approved = true
	verdict.Summary = llmResult.Feedback
	return verdict, nil
}

// collectDiff runs `git add -N .` followed by `git diff HEAD` to produce
// a unified diff that includes both modified and untracked files. It
// also returns the list of touched files parsed from `git status --porcelain`.
func collectDiff(ctx context.Context, repoDir string) (string, []string, error) {
	addCmd := exec.CommandContext(ctx, "git", "add", "-N", ".")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("git add -N: %w\n%s", err, out)
	}

	diffCmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diffCmd.Dir = repoDir
	var diffOut bytes.Buffer
	diffCmd.Stdout = &diffOut
	var diffStderr bytes.Buffer
	diffCmd.Stderr = &diffStderr
	if err := diffCmd.Run(); err != nil {
		return "", nil, fmt.Errorf("git diff HEAD: %w\n%s", err, diffStderr.String())
	}
	diff := diffOut.String()
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n... (diff truncated)"
	}

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git status: %w", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			files = append(files, parts[len(parts)-1])
		}
	}
	return diff, files, nil
}
```

- [ ] **Step 4: Run tests to verify short-circuit tests pass**

Run: `go test ./internal/codereviewer/ -run TestReview_ShortCircuits -v`
Expected: PASS for both. Short-circuit on build/vet failure returns before reaching the semantic stub, so the `nil` `reviewSemantics` var is never invoked.

Also run the whole package to confirm nothing regressed:

Run: `go test ./internal/codereviewer/ -v`
Expected: all prior tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/reviewer.go internal/codereviewer/reviewer_test.go
git commit -m "feat(codereviewer): add Review with build/vet short-circuit (#33)"
```

---

## Task 7: Gemini Flash semantic call

**Files:**
- Modify: `internal/codereviewer/reviewer.go`

No unit test in this task — mocking the Gemini SDK would be disproportionate (see spec § Testing). Task 8 adds the integration test. The short-circuit tests from Task 6 continue to pass because they short-circuit before the LLM.

- [ ] **Step 1: Extend the import block**

Update the import block in `internal/codereviewer/reviewer.go` by adding `encoding/json`, `llmutil`, and `genai`. The final block should read:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llmutil"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"google.golang.org/genai"
)
```

- [ ] **Step 2: Replace the `reviewSemantics` declaration with a real initializer and add `callGeminiForReview`**

In `reviewer.go`, find this line added in Task 6:

```go
var reviewSemantics func(ctx context.Context, apiKey, prompt string) (*llmVerdict, int, int, error)
```

Replace it with:

```go
// reviewSemantics is a package var so tests can replace it with a stub.
// Default is the real Gemini Flash call in callGeminiForReview.
var reviewSemantics = callGeminiForReview

// callGeminiForReview makes a single gemini-2.5-flash call with the
// system prompt and user prompt, parses the JSON response, and returns
// the verdict plus token usage.
func callGeminiForReview(ctx context.Context, apiKey, prompt string) (*llmVerdict, int, int, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("creating genai client: %w", err)
	}

	resp, err := client.Models.GenerateContent(ctx, geminiModel, genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{
			genai.NewPartFromText(codeReviewSystemPrompt),
		}},
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("generating content: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, 0, 0, fmt.Errorf("empty response from model")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
	}

	var out llmVerdict
	if err := json.Unmarshal([]byte(llmutil.CleanJSON(text)), &out); err != nil {
		return nil, 0, 0, fmt.Errorf("parsing JSON (%q): %w", text, err)
	}

	var inTok, outTok int
	if resp.UsageMetadata != nil {
		inTok = int(resp.UsageMetadata.PromptTokenCount)
		outTok = int(resp.UsageMetadata.CandidatesTokenCount)
	}
	return &out, inTok, outTok, nil
}
```

The `cost.Calculate` call in `Review` and the `geminiModel` constant are already in place from Task 6 — no changes needed there.

- [ ] **Step 3: Run the full package test suite**

Run: `go test ./internal/codereviewer/ -v`
Expected: all tests pass. Short-circuit tests still pass (build/vet fail before the LLM is reached). No new test is introduced in this task.

Also run a broader build to catch import cycles:

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/codereviewer/reviewer.go
git commit -m "feat(codereviewer): wire real Gemini Flash semantic review call (#33)"
```

---

## Task 8: Integration test (skipped without `GOOGLE_API_KEY`)

**Files:**
- Modify: `internal/codereviewer/reviewer_test.go`

- [ ] **Step 1: Add the integration test**

Append to `reviewer_test.go`:

```go
// TestReview_Integration runs Review end-to-end against a live
// Gemini Flash model. Skipped under `-short` and when GOOGLE_API_KEY
// (or GEMINI_API_KEY) is not set.
//
// Run manually with:
//
//	GOOGLE_API_KEY=... go test ./internal/codereviewer -run Integration -v
func TestReview_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	geminiKey := os.Getenv("GOOGLE_API_KEY")
	if geminiKey == "" {
		geminiKey = os.Getenv("GEMINI_API_KEY")
	}
	if geminiKey == "" {
		t.Skip("GOOGLE_API_KEY / GEMINI_API_KEY not set")
	}

	// Initialize a git repo so collectDiff can run.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	gitInit(t, dir)
	gitCommitAll(t, dir, "initial")

	// Introduce a deliberately stubbed change.
	mustWrite(t, filepath.Join(dir, "main.go"), `package main

func main() {
	// TODO: implement the actual fix described in the plan
	_ = 1
}
`)

	issue := &github.Issue{
		Number: 42,
		Title:  "Add error handling to main",
		Body:   "main() should validate inputs and return a non-zero exit code on error.",
	}
	plan := &planner.ImplementationPlan{
		Markdown: "## Task\n\nAdd full error handling to main.go. Validate arguments and return non-zero on failure.",
	}
	dossier := &archivist.Dossier{
		Summary:  "main.go has no error handling today",
		Approach: "Wrap the body in a run() helper returning error",
	}

	verdict, err := Review(context.Background(), geminiKey, dir, issue, plan, dossier)
	if err != nil {
		t.Fatalf("Review error: %v", err)
	}
	if verdict.Approved {
		t.Errorf("expected rejection (diff contains stub + TODO), got approved. Summary: %s", verdict.Summary)
	}
	if verdict.Category != "semantic" {
		t.Errorf("expected Category=semantic, got %q", verdict.Category)
	}
	if verdict.InputTokens == 0 || verdict.OutputTokens == 0 {
		t.Errorf("expected non-zero token counts, got in=%d out=%d", verdict.InputTokens, verdict.OutputTokens)
	}
	if verdict.CostUSD <= 0 {
		t.Errorf("expected positive CostUSD, got %f", verdict.CostUSD)
	}
}

// gitInit and gitCommitAll are test helpers that wrap `git init` / add / commit
// so the integration test has a HEAD to diff against. Kept in the _test
// file because the production code never needs to init a repo.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "init", "--quiet"},
		{"git", "-c", "user.email=t@t.test", "-c", "user.name=t", "config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "-c", "user.email=t@t.test", "-c", "user.name=t", "commit", "-m", msg, "--quiet"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}
```

Also add `"os/exec"` to the test file's import block:

```go
import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)
```

- [ ] **Step 2: Run the suite under `-short` to confirm it's skipped**

Run: `go test ./internal/codereviewer/ -short -v`
Expected: `TestReview_Integration` reports `SKIP`; all other tests PASS.

- [ ] **Step 3: Optional manual run**

Only if `GOOGLE_API_KEY` is set:

Run: `go test ./internal/codereviewer/ -run Integration -v`
Expected: PASS, verdict `Category=semantic`, non-zero token counts, positive cost. Costs ~$0.0013 per invocation.

If it fails, read the verdict summary — Gemini may have approved the stubbed code if the prompt isn't strict enough about TODOs. Tune the stubbed main.go to be more obviously broken (e.g., add `// STUB: do nothing` and remove all real logic) rather than loosening the system prompt.

- [ ] **Step 4: Commit**

```bash
git add internal/codereviewer/reviewer_test.go
git commit -m "test(codereviewer): add live Gemini integration test (#33)"
```

---

## Task 9: `writeCodeReviewArtifact` helper in `cmd/implementer/main.go`

**Files:**
- Modify: `cmd/implementer/main.go` (add helper at the bottom of the file, near existing `appendPRURL` at L508-526)

- [ ] **Step 1: Add the import**

In `cmd/implementer/main.go`, add the new package to the import block:

```go
import (
	// ... existing imports ...
	"github.com/mjhilldigital/conduit-agent-experiment/internal/codereviewer"
	// ... rest of existing imports ...
)
```

The full existing import block is at `cmd/implementer/main.go:3-25`; insert `codereviewer` alphabetically between `cost` and `github`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/codereviewer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/implementer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/responder"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/triage"
)
```

- [ ] **Step 2: Add the `writeCodeReviewArtifact` helper**

Append the following to `cmd/implementer/main.go` (bottom of file, after `fetchPRComments` at L528-541):

```go
// writeCodeReviewArtifact merges the code review verdict into
// run-summary.json under a "code_review" key. Mirrors the appendPRURL
// pattern: read JSON, merge, write back. No-op when dir is empty.
//
// retried indicates whether the retry path was consumed during this
// run (true = this verdict is the result of the second attempt).
func writeCodeReviewArtifact(dir string, verdict *codereviewer.Verdict, retried bool) {
	if dir == "" || verdict == nil {
		return
	}
	path := filepath.Join(dir, "run-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: failed to read run-summary.json for code-review update: %v", err)
		return
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		log.Printf("Warning: failed to parse run-summary.json: %v", err)
		return
	}
	summary["code_review"] = map[string]any{
		"approved":        verdict.Approved,
		"category":        verdict.Category,
		"summary":         verdict.Summary,
		"retried":         retried,
		"build_passed":    verdict.Category != "build",
		"vet_passed":      verdict.Category != "build" && verdict.Category != "vet",
		"semantic_result": verdict.SemanticResult,
		"input_tokens":    verdict.InputTokens,
		"output_tokens":   verdict.OutputTokens,
		"cost_usd":        verdict.CostUSD,
	}
	updated, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		log.Printf("Warning: failed to marshal updated run-summary: %v", err)
		return
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		log.Printf("Warning: failed to write updated run-summary: %v", err)
	}
}
```

Note on `build_passed` / `vet_passed`: these are derived from `Category` rather than stored separately, because `Verdict` already tracks the failing category unambiguously. On approval, `Category == ""` → both true. On build failure, build_passed = false, vet_passed = false (vet wasn't run). On vet failure, build_passed = true, vet_passed = false.

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/implementer/...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): add writeCodeReviewArtifact helper (#33)"
```

---

## Task 10: Wire step 9a into `cmd/implementer/main.go`

**Files:**
- Modify: `cmd/implementer/main.go`

This is the user-visible change — the new gate runs on every pipeline invocation.

- [ ] **Step 1: Insert step 9a between existing step 9 and step 10**

In `cmd/implementer/main.go`, locate the existing step 9 (`// 9. Check for changes...` at L197) and step 10 (`// 10. Create or update branch and draft PR...` at L223). Insert the new block between them.

The exact insertion point is immediately after the existing logging block at L218-221:

```go
	if len(diffOutput) > 0 {
		log.Printf("Changes:\n%s", string(diffOutput))
	}
	if len(statusOutput) > 0 {
		log.Printf("Status:\n%s", string(statusOutput))
	}
```

And immediately before:

```go
	// 10. Create or update branch and draft PR (handles collisions)
```

Insert:

```go
	// 9a. Internal code review (re-runs go build/vet externally and
	// makes one Gemini Flash call for semantic checks). See #33.
	artifactDir := os.Getenv("IMPL_ARTIFACT_DIR")
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
				"\n\nFix the issues above. The build and vet checks will be re-run.",
		}

		retryResult, rerr := implementer.RunAgent(ctx, anthropicKey, modelName,
			repoDir, retryPlan, maxIter, implMaxCost)
		if rerr != nil {
			writeCodeReviewArtifact(artifactDir, verdict, true)
			log.Fatalf("implementer retry failed: %v", rerr)
		}
		log.Printf("Retry completed in %d iterations", retryResult.Iterations)

		// Merge retry token counts into the primary result so
		// run-summary.json reflects total implementer spend.
		result.Iterations += retryResult.Iterations
		result.InputTokens += retryResult.InputTokens
		result.OutputTokens += retryResult.OutputTokens
		result.CacheCreationTokens += retryResult.CacheCreationTokens
		result.CacheReadTokens += retryResult.CacheReadTokens
		if retryResult.BudgetExceeded {
			result.BudgetExceeded = true
		}

		if result.BudgetExceeded {
			log.Printf("Implementer budget exceeded during retry (IMPL_MAX_COST=$%.4f) — halting", implMaxCost)
			writeCodeReviewArtifact(artifactDir, verdict, true)
			os.RemoveAll(repoDir)
			os.RemoveAll(dossierDir)
			os.Exit(1)
		}

		// Re-review after the retry.
		verdict, err = codereviewer.Review(ctx, geminiKey, repoDir, fullIssue, plan, dossier)
		if err != nil {
			writeCodeReviewArtifact(artifactDir, verdict, true)
			log.Fatalf("code reviewer (retry) failed: %v", err)
		}
		log.Printf("Retry code review verdict: approved=%v category=%q summary=%s",
			verdict.Approved, verdict.Category, verdict.Summary)
		if !verdict.Approved {
			log.Printf("Code review still rejected after retry: %s", verdict.Feedback)
			writeCodeReviewArtifact(artifactDir, verdict, true)
			log.Fatalf("halting before PR creation — code review failed twice")
		}
		log.Printf("Code review approved after retry")
	}

	// Record the final (approved) verdict. Must happen before step 10
	// so the artifact reflects review state even if PR upsert fails.
	writeCodeReviewArtifact(artifactDir, verdict, false)

```

- [ ] **Step 2: Remove the duplicate `artifactDir` inline lookups**

The existing code at `cmd/implementer/main.go:186-188` and `cmd/implementer/main.go:262-264` reads `os.Getenv("IMPL_ARTIFACT_DIR")` inline. Now that we bind it once at step 9a, reuse the local variable in those other spots **only if** they come after step 9a. Since L186 and L262 are *before* and *after* step 9a respectively, and step 9a introduces the var, the simplest solution is to bind `artifactDir` earlier so both existing sites and the new step can share it.

Move the `artifactDir := os.Getenv("IMPL_ARTIFACT_DIR")` line to just after step 8's budget check (currently L195, right after the `os.Exit(1)` block for budget_exceeded). Delete the inline lookups at L186 and L262.

Specifically:

1. At `cmd/implementer/main.go:185-188`, the existing code is:

```go
	// Write run artifacts for CI (GitHub Actions artifact upload)
	if artifactDir := os.Getenv("IMPL_ARTIFACT_DIR"); artifactDir != "" {
		writeRunArtifacts(artifactDir, issue, result, modelName, plan)
	}
```

Change to:

```go
	// Write run artifacts for CI (GitHub Actions artifact upload)
	artifactDir := os.Getenv("IMPL_ARTIFACT_DIR")
	if artifactDir != "" {
		writeRunArtifacts(artifactDir, issue, result, modelName, plan)
	}
```

2. In the new step 9a block, **delete** the line `artifactDir := os.Getenv("IMPL_ARTIFACT_DIR")` — it's already in scope.

3. At `cmd/implementer/main.go:261-265` (originally), existing:

```go
	// Update artifact with PR URL (skipped when no PR was created)
	if prURL != "" {
		if artifactDir := os.Getenv("IMPL_ARTIFACT_DIR"); artifactDir != "" {
			appendPRURL(artifactDir, prURL)
		}
	}
```

Change to:

```go
	// Update artifact with PR URL (skipped when no PR was created)
	if prURL != "" && artifactDir != "" {
		appendPRURL(artifactDir, prURL)
	}
```

- [ ] **Step 3: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: success. If the compiler complains about an unused variable, it usually means `artifactDir` is bound but never read in a branch — walk through the control flow and confirm all three usages are reachable.

- [ ] **Step 4: Run the full unit test suite (not just the new package)**

Run: `go test ./... -short`
Expected: all tests PASS. The new code in `main.go` has no tests but must not break any existing tests (including planner, implementer, hitl, responder packages).

- [ ] **Step 5: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): add code review gate as pipeline step 9a (#33)"
```

---

## Task 11: Final verification and cleanup

**Files:**
- None (verification only)

- [ ] **Step 1: Confirm the final file list matches the plan**

Run: `git diff --stat main...HEAD -- internal/codereviewer cmd/implementer`
Expected files changed:
- `internal/codereviewer/types.go` (new)
- `internal/codereviewer/checks.go` (new)
- `internal/codereviewer/reviewer.go` (new)
- `internal/codereviewer/reviewer_test.go` (new)
- `cmd/implementer/main.go` (modified)

Nothing else should have drifted.

- [ ] **Step 2: Run the full test suite under `-short`**

Run: `go test ./... -short -v`
Expected: all tests pass, `TestReview_Integration` skips.

- [ ] **Step 3: Run vet on the whole repo**

Run: `go vet ./...`
Expected: no warnings.

- [ ] **Step 4 (optional but recommended): Manual smoke test against a known-broken target**

If `ANTHROPIC_API_KEY` and `GOOGLE_API_KEY` are set, run the pipeline against an issue known to produce a broken implementation in prior runs — e.g., an issue the dashboard shows as previously failed with `hallucinated symbols`. Expected: the new gate either catches and retries successfully, or halts with a `code_review` block in the artifact showing the failure category. Check `run-summary.json`:

```bash
cat $IMPL_ARTIFACT_DIR/run-summary.json | jq '.code_review'
```

Expected shape on approval:

```json
{
  "approved": true,
  "category": "",
  "build_passed": true,
  "vet_passed": true,
  "retried": false,
  "semantic_result": "...",
  "input_tokens": 7000,
  "output_tokens": 140,
  "cost_usd": 0.00115
}
```

If the manual smoke test is not run, note that in the PR description so reviewers know integration coverage was unit-test only.

- [ ] **Step 5: Open PR**

Use the standard PR template for this repo. The PR should reference issue #33, summarize the new gate, and call out:
- Spec: `docs/specs/2026-04-09-internal-code-reviewer-design.md`
- New package: `internal/codereviewer/`
- Cost impact: +~$0.0013 per run for the Gemini Flash semantic call; potentially negative-cost overall if it prevents wasted responder iterations.
- No feature flag — the gate is always on.

No commit for this task — it's verification only.

---

## Follow-ups (explicitly out of scope)

These are recorded so they don't get quietly absorbed. Each should be a separate issue or separate PR:

- Dashboard UI surfacing of `code_review` block in `docs/dashboard/index.html` — the artifact shape supports it, but the UI change is not in this plan.
- Issue #34 (target-repo linter) — complementary syntactic gate that should slot in alongside this one. If starting #34 before this lands, coordinate the step-9a insertion point.
- Telemetry on retry frequency — once we have a few runs, tune the retry-vs-halt ratio.
- Narrowing `go vet ./...` to only changed packages if unrelated false positives become a problem.
