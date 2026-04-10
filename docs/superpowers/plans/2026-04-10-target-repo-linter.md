# Target-Repo Linter Implementation Plan

> **⚠️ HISTORICAL PLAN — do not re-execute verbatim.** This document captures the plan as it looked at plan-time (2026-04-10, before any code was written). The shipped implementation diverged during execution and subsequent review rounds — notably:
>
> - **Task 5 `RunLint`** was refactored in commit `ea8e9ba` to call the shared `runBoundedCmd` helper in `checks.go` instead of building its own `exec.Cmd` with inline env/buffer/truncation logic. The code block in Task 5 Step 3 shows the pre-refactor shape.
> - **Task 7 `Review()` wiring** was amended in commits `e1940de` (added `repoDir` to `filterLintErrors` for absolute-path normalization) and `c665d76` (added `truncated` return and the fail-closed path for unparseable/capped output, plus a new `formatLintRawFeedback` renderer). The decision-logic pseudocode in Task 7 Step 3 shows only the original kept/dropped branches.
> - **Task 1** claimed no changes to `cmd/implementer/main.go`; in `e1940de` the `writeCodeReviewArtifact` function was updated to add the lint fields to its hand-curated JSON map and to teach `buildPassed`/`vetPassed` about `Category="lint"`.
> - **Task 6 `collectChangedFiles`** gained a robust porcelain-v1 parser in commit `02c095e` that handles filenames with spaces and renames (`strings.Fields` would have silently dropped them).
> - **Task 4 Makefile probe** tightened the regex to `(?m)^lint::?(?:\s|$)` and learned GNU make's file precedence (`GNUmakefile` > `makefile` > `Makefile`) in commit `02c095e`.
>
> For the current truth, read the code under `internal/codereviewer/` and the spec at `docs/superpowers/specs/2026-04-10-target-repo-linter-design.md` (kept in sync as the implementation evolved). This plan remains useful as a record of how the feature was broken down, what the original design intent was, and which tasks were bite-sized enough to dispatch to subagents — but the code snippets should be read as plan-time intent, not authoritative.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a target-repo linter as a new deterministic check inside `internal/codereviewer`, sharing the existing one-retry loop and feeding changed-file-filtered feedback back to the implementer on failure.

**Architecture:** Extend `Review()` with a new `RunLint` step between `go vet` and the Gemini semantic call. Lint is Go-only v1, preferring the repo's `make lint` target and falling back to `golangci-lint run --out-format=line-number`. Failures are filtered via a parser that only keeps errors in files the agent touched — pre-existing lint debt becomes an advisory pass, not a pipeline halt.

**Tech Stack:** Go 1.21+, `os/exec`, existing `cappedBuffer` / `runGo` pattern from `checks.go`, existing `reviewSemantics` stubbing pattern from `reviewer.go`.

**Spec:** `docs/superpowers/specs/2026-04-10-target-repo-linter-design.md`

---

## File Structure

**Files created:**
- `internal/codereviewer/linter.go` — `lintConfig`, `lintError`, `detectLinter`, `RunLint`, `filterLintErrors`, `formatLintFeedback`, and the `lookPathFn` package var for stubbing
- `internal/codereviewer/linter_test.go` — unit tests for all of the above

**Files modified:**
- `internal/codereviewer/types.go` — add `LintOutput`, `LintErrorsKept`, `LintErrorsDropped` to `Verdict`
- `internal/codereviewer/reviewer.go` — extract `collectChangedFiles` helper, wire `RunLint` into `Review()`, add `runLintFn` package var for stubbing
- `internal/codereviewer/reviewer_test.go` — add 4 integration test cases exercising the new wiring

No changes to `cmd/implementer/main.go`'s retry block — it already consumes `verdict.Feedback` unchanged.

> **Retrospective note:** during the final review pass, `cmd/implementer/main.go` *was* modified after all — `writeCodeReviewArtifact` turned out to build a hand-curated `map[string]any` rather than JSON-serialize the `Verdict` struct, so the new lint fields had to be added to the map explicitly, and `buildPassed` / `vetPassed` had to learn the new `"lint"` category. See commit `e1940de`. The retry block itself still wasn't touched.

---

## Task 1: Add Verdict fields for lint telemetry

**Files:**
- Modify: `internal/codereviewer/types.go`

Pure struct-field addition — no new behavior to test. The fields land empty-valued on every existing code path until Task 7 wires them.

- [ ] **Step 1: Add the three new fields to `Verdict`**

In `internal/codereviewer/types.go`, add these fields alongside the existing `BuildOutput`, `VetOutput`, `SemanticResult`:

```go
// Diagnostics — populated even on approval, for the artifact.
BuildOutput    string `json:"build_output,omitempty"`
VetOutput      string `json:"vet_output,omitempty"`
LintOutput     string `json:"lint_output,omitempty"`
SemanticResult string `json:"semantic_result,omitempty"`

// Lint telemetry — populated when RunLint ran. Kept counts errors in
// changed files (actionable); Dropped counts parsed errors in unchanged
// files (treated as pre-existing debt and ignored for retry decisions).
LintErrorsKept    int `json:"lint_errors_kept"`
LintErrorsDropped int `json:"lint_errors_dropped"`
```

Place `LintOutput` between `VetOutput` and `SemanticResult` to preserve the existing ordering of build → vet → semantic diagnostics.

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/codereviewer/...`
Expected: clean build, no output.

- [ ] **Step 3: Verify existing tests still pass**

Run: `go test ./internal/codereviewer/... -short`
Expected: all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/codereviewer/types.go
git commit -m "feat(codereviewer): add lint telemetry fields to Verdict (#34)

Part 1 of target-repo linter gate. Adds LintOutput, LintErrorsKept,
and LintErrorsDropped to Verdict so the artifact and dashboard can
track lint results. Fields are empty on every existing code path
until the lint step is wired in a later commit."
```

---

## Task 2: `filterLintErrors` (pure function, TDD)

**Files:**
- Create: `internal/codereviewer/linter.go`
- Create: `internal/codereviewer/linter_test.go`

The parser is a pure function and the backbone of the "don't block on pre-existing debt" decision. TDD first so the regex gets exercised against every realistic format before it ships.

- [ ] **Step 1: Write the failing table test**

Create `internal/codereviewer/linter_test.go` with:

```go
package codereviewer

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterLintErrors(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		changedFiles []string
		wantKept     []lintError
		wantDropped  int
	}{
		{
			name: "canonical golangci-lint format with mixed files",
			output: "internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
				"internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
				"internal/unchanged/baz.go:11:1: var X should be Y (stylecheck)\n",
			changedFiles: []string{"internal/foo/bar.go"},
			wantKept: []lintError{
				{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
				{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
			},
			wantDropped: 1,
		},
		{
			name:         "no column (line-only)",
			output:       "foo.go:42: some message\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 42, Col: 0, Message: "some message"},
			},
			wantDropped: 0,
		},
		{
			name:         "path normalization strips leading ./",
			output:       "./internal/foo.go:10:1: msg\n",
			changedFiles: []string{"internal/foo.go"},
			wantKept: []lintError{
				{File: "internal/foo.go", Line: 10, Col: 1, Message: "msg"},
			},
			wantDropped: 0,
		},
		{
			name: "non-matching garbage lines are ignored (not counted)",
			output: "level=warning msg=\"something unrelated\"\n" +
				"make: *** [lint] Error 1\n" +
				"foo.go:1:1: real error\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 1, Col: 1, Message: "real error"},
			},
			wantDropped: 0,
		},
		{
			name: "parser cap at 500 lines",
			output: func() string {
				var sb strings.Builder
				for i := 0; i < 1000; i++ {
					sb.WriteString("unchanged.go:1:1: msg\n")
				}
				return sb.String()
			}(),
			changedFiles: []string{"something_else.go"},
			wantKept:     nil,
			wantDropped:  500, // only 500 are parsed; the rest are not seen
		},
		{
			name:         "empty output",
			output:       "",
			changedFiles: []string{"foo.go"},
			wantKept:     nil,
			wantDropped:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped := filterLintErrors(tc.output, tc.changedFiles)
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept:\n  got:  %#v\n  want: %#v", kept, tc.wantKept)
			}
			if dropped != tc.wantDropped {
				t.Errorf("dropped: got %d, want %d", dropped, tc.wantDropped)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codereviewer/... -run TestFilterLintErrors -v`
Expected: FAIL with `undefined: filterLintErrors` and `undefined: lintError`.

- [ ] **Step 3: Implement `lintError` and `filterLintErrors` in `linter.go`**

Create `internal/codereviewer/linter.go` with:

```go
package codereviewer

import (
	"regexp"
	"strconv"
	"strings"
)

// lintError is a single parsed diagnostic from a Go linter's output.
// Col is 0 when the linter emitted only file:line without a column.
type lintError struct {
	File    string
	Line    int
	Col     int
	Message string
}

// lintParseCap bounds how many error lines the parser will accept before
// giving up. Pathological output (thousands of errors from a misconfigured
// lint run) should not balloon memory or CPU.
const lintParseCap = 500

// lintLineRE matches the canonical Go linter output format:
//
//	file:line:col: message
//	file:line: message
//
// It deliberately rejects lines starting with non-file prefixes like
// "level=" or "make:" by anchoring file to a non-colon run followed by
// a required :<digits>: pattern.
var lintLineRE = regexp.MustCompile(`^([^:]+):(\d+):(?:(\d+):)?\s*(.+)$`)

// filterLintErrors parses lint output and returns only errors whose file
// path falls within changedFiles. The second return is the count of
// parsed-but-dropped errors (errors in unchanged files — pre-existing
// debt we do not want to retry on).
//
// Lines that do not match lintLineRE are ignored entirely, not counted
// as dropped, because they were never parsed as errors. Parsing stops
// after lintParseCap matched lines.
func filterLintErrors(output string, changedFiles []string) ([]lintError, int) {
	changed := make(map[string]struct{}, len(changedFiles))
	for _, f := range changedFiles {
		changed[normalizeLintPath(f)] = struct{}{}
	}

	var kept []lintError
	dropped := 0
	parsed := 0

	for _, line := range strings.Split(output, "\n") {
		if parsed >= lintParseCap {
			break
		}
		m := lintLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		parsed++

		file := normalizeLintPath(m[1])
		lineNum, _ := strconv.Atoi(m[2])
		col := 0
		if m[3] != "" {
			col, _ = strconv.Atoi(m[3])
		}
		msg := m[4]

		if _, ok := changed[file]; ok {
			kept = append(kept, lintError{
				File:    file,
				Line:    lineNum,
				Col:     col,
				Message: msg,
			})
		} else {
			dropped++
		}
	}

	return kept, dropped
}

// normalizeLintPath strips a leading "./" so that a linter emitting
// "./internal/foo.go" matches a changedFiles entry of "internal/foo.go".
// Absolute-path normalization against repoDir is handled by the caller
// before passing paths into filterLintErrors.
func normalizeLintPath(p string) string {
	return strings.TrimPrefix(p, "./")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/codereviewer/... -run TestFilterLintErrors -v`
Expected: all 6 subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/linter.go internal/codereviewer/linter_test.go
git commit -m "feat(codereviewer): add filterLintErrors parser (#34)

Parses golangci-lint and generic Go linter output into structured
lintError records and filters by a set of changed files. Errors in
unchanged files are counted as 'dropped' so pre-existing lint debt
can be distinguished from agent-introduced issues. Parser capped at
500 lines to bound pathological output. No callers yet."
```

---

## Task 3: `formatLintFeedback` (golden test)

**Files:**
- Modify: `internal/codereviewer/linter.go`
- Modify: `internal/codereviewer/linter_test.go`

- [ ] **Step 1: Write the failing golden test**

Append to `internal/codereviewer/linter_test.go`:

```go
func TestFormatLintFeedback(t *testing.T) {
	errs := []lintError{
		{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
		{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
		{File: "cmd/implementer/main.go", Line: 301, Col: 12, Message: "exported function Frob should have comment (golint)"},
	}

	want := "## Lint Errors\n\n" +
		"The following lint violations were introduced by your changes. Fix each one:\n\n" +
		"- internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
		"- internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
		"- cmd/implementer/main.go:301:12: exported function Frob should have comment (golint)\n\n" +
		"Re-run the build and try again."

	got := formatLintFeedback(errs)
	if got != want {
		t.Errorf("formatLintFeedback mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codereviewer/... -run TestFormatLintFeedback -v`
Expected: FAIL with `undefined: formatLintFeedback`.

- [ ] **Step 3: Implement `formatLintFeedback`**

Append to `internal/codereviewer/linter.go`:

```go
// formatLintFeedback renders a slice of lintError records into the
// markdown body that Review() stores in verdict.Feedback on a lint
// rejection. cmd/implementer/main.go appends verdict.Feedback to the
// retry plan unchanged, so the format here is what the implementer
// actually reads on the retry pass.
//
// Col == 0 is rendered without the trailing ":0" so line-only linter
// output reads naturally. (The current v1 caller always passes col > 0
// from the canonical golangci-lint format, but the fallback keeps the
// output clean if a future linter omits columns.)
func formatLintFeedback(errs []lintError) string {
	var b strings.Builder
	b.WriteString("## Lint Errors\n\n")
	b.WriteString("The following lint violations were introduced by your changes. Fix each one:\n\n")
	for _, e := range errs {
		if e.Col > 0 {
			b.WriteString("- " + e.File + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ": " + e.Message + "\n")
		} else {
			b.WriteString("- " + e.File + ":" + strconv.Itoa(e.Line) + ": " + e.Message + "\n")
		}
	}
	b.WriteString("\nRe-run the build and try again.")
	return b.String()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/codereviewer/... -run TestFormatLintFeedback -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/linter.go internal/codereviewer/linter_test.go
git commit -m "feat(codereviewer): add formatLintFeedback renderer (#34)

Converts a slice of lintError into the markdown body that Review()
will store in verdict.Feedback for the retry path. Golden test locks
the exact wording so accidental prompt drift is caught in CI."
```

---

## Task 4: `detectLinter`

**Files:**
- Modify: `internal/codereviewer/linter.go`
- Modify: `internal/codereviewer/linter_test.go`

Introduces the `lintConfig` type, the `lookPathFn` stubbing hook, and env-var escape hatch. Tested against filesystem fixtures; `exec.LookPath` is stubbed.

- [ ] **Step 1: Write the failing tests**

Append to `internal/codereviewer/linter_test.go`:

```go
func TestDetectLinter(t *testing.T) {
	// Stub lookPathFn for the whole test so we don't depend on the host
	// environment having (or not having) golangci-lint installed.
	t.Cleanup(func() { lookPathFn = exec.LookPath })

	t.Run("makefile with lint target wins", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "build:\n\tgo build ./...\n\nlint:\n\tgolangci-lint run\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != "make" {
			t.Errorf("expected Mode=make, got %+v", cfg)
		}
	})

	t.Run("makefile without lint target falls through", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "build:\n\tgo build ./...\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil config, got %+v", cfg)
		}
	})

	t.Run("golangci-lint with config on path", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "/usr/local/bin/golangci-lint", nil }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != "golangci-lint" {
			t.Errorf("expected Mode=golangci-lint, got %+v", cfg)
		}
		if !strings.HasSuffix(cfg.ConfigPath, ".golangci.yml") {
			t.Errorf("expected ConfigPath to end with .golangci.yml, got %q", cfg.ConfigPath)
		}
	})

	t.Run("golangci config but no binary returns nil", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil, got %+v", cfg)
		}
	})

	t.Run("empty repo returns nil", func(t *testing.T) {
		dir := t.TempDir()
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil, got %+v", cfg)
		}
	})

	t.Run("AGENT_LINT=off short circuits", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "lint:\n\techo ok\n")
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "/usr/local/bin/golangci-lint", nil }
		t.Setenv("AGENT_LINT", "off")

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (AGENT_LINT=off), got %+v", cfg)
		}
	})

	t.Run("makefile larger than 64 KiB only scans first 64 KiB", func(t *testing.T) {
		dir := t.TempDir()
		var sb strings.Builder
		sb.WriteString("build:\n\tgo build\n\n")
		// Pad with a harmless 'other' target until we push past 64 KiB,
		// then append a lint target that lives beyond the cap.
		padding := strings.Repeat("other:\n\techo nope\n\n", 5000) // ~95 KiB
		sb.WriteString(padding)
		sb.WriteString("lint:\n\techo beyond cap\n")
		mustWrite(t, filepath.Join(dir, "Makefile"), sb.String())
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (lint target beyond 64 KiB cap), got %+v", cfg)
		}
	})
}
```

Also add the missing imports to `linter_test.go`'s import block: `"os/exec"`, `"path/filepath"`. (`strings` is already imported from Task 2.)

Note: `mustWrite` is defined in `reviewer_test.go` at line 28 — same package, so no redeclaration.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codereviewer/... -run TestDetectLinter -v`
Expected: FAIL with `undefined: lookPathFn`, `undefined: detectLinter`, `undefined: lintConfig`.

- [ ] **Step 3: Implement `detectLinter` and supporting types**

Append to `internal/codereviewer/linter.go`:

```go
// Add to the existing import block at the top of linter.go:
//	"io"
//	"os"
//	"os/exec"
//	"path/filepath"

// lintConfig describes which linter RunLint should invoke.
type lintConfig struct {
	Mode       string // "make" or "golangci-lint"
	ConfigPath string // empty for make mode; path to .golangci.* for golangci-lint mode
}

// lintMakefileScanCap bounds how many bytes of a target repo's Makefile
// we inspect when probing for a lint target. 64 KiB is vastly larger
// than any real Makefile and small enough that a pathological fixture
// cannot blow up memory or CPU.
const lintMakefileScanCap = 64 * 1024

// lintMakeTargetRE matches a line like `lint:` or `lint: deps` at the
// beginning of a line. \n anchors so we do not match `prelint:` etc.
var lintMakeTargetRE = regexp.MustCompile(`(?m)^lint:`)

// lookPathFn is exec.LookPath by default, exposed as a package var so
// tests can stub binary discovery without touching the host PATH.
var lookPathFn = exec.LookPath

// detectLinter probes repoDir for the linter RunLint should use.
// Returns (nil, nil) when no lint workflow is available — callers
// should treat this as a silent no-op, not an error.
//
// Order of precedence:
//  1. AGENT_LINT=off env var short-circuits to nil (operator kill switch)
//  2. Makefile with a `^lint:` target → {Mode: "make"}
//  3. golangci-lint binary on PATH AND a .golangci.{yml,yaml,toml}
//     config in repoDir → {Mode: "golangci-lint", ConfigPath: ...}
//  4. Otherwise → nil
func detectLinter(repoDir string) (*lintConfig, error) {
	if strings.EqualFold(os.Getenv("AGENT_LINT"), "off") {
		return nil, nil
	}

	// Makefile probe. Read bounded prefix to protect against pathological
	// Makefiles (and for symmetry with the rest of the package, which
	// caps all external input).
	if f, err := os.Open(filepath.Join(repoDir, "Makefile")); err == nil {
		buf := make([]byte, lintMakefileScanCap)
		n, rerr := io.ReadFull(f, buf)
		f.Close()
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("reading Makefile: %w", rerr)
		}
		if lintMakeTargetRE.Match(buf[:n]) {
			return &lintConfig{Mode: "make"}, nil
		}
	}

	// golangci-lint probe. Requires both the binary AND a config file —
	// running golangci-lint without a config in a repo that hasn't opted
	// into it would be presumptuous and noisy.
	for _, name := range []string{".golangci.yml", ".golangci.yaml", ".golangci.toml"} {
		cfgPath := filepath.Join(repoDir, name)
		if _, err := os.Stat(cfgPath); err == nil {
			if _, err := lookPathFn("golangci-lint"); err == nil {
				return &lintConfig{Mode: "golangci-lint", ConfigPath: cfgPath}, nil
			}
			// Config found but binary missing — fall through to nil so
			// the pipeline skips lint rather than halting.
			break
		}
	}

	return nil, nil
}
```

Add `fmt` and the new imports (`io`, `os`, `os/exec`, `path/filepath`) to the `linter.go` import block. The existing imports from Tasks 2–3 are `regexp`, `strconv`, `strings`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/codereviewer/... -run TestDetectLinter -v`
Expected: all 7 subtests PASS.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/codereviewer/... -short`
Expected: all tests pass (no regressions in prior tasks).

- [ ] **Step 6: Commit**

```bash
git add internal/codereviewer/linter.go internal/codereviewer/linter_test.go
git commit -m "feat(codereviewer): add detectLinter with Makefile + golangci-lint probes (#34)

Probes a cloned target repo for its lint workflow: prefers a
\`make lint\` target, falls back to golangci-lint + a repo-local
config file. Returns nil for no-op (no lint configured, binary
missing, or AGENT_LINT=off operator kill switch). lookPathFn is a
package var so tests can stub binary discovery without touching
host PATH."
```

---

## Task 5: `RunLint`

**Files:**
- Modify: `internal/codereviewer/linter.go`
- Modify: `internal/codereviewer/linter_test.go`

Wraps `detectLinter` + command execution behind a single entry point that `Review()` will call. Mirrors the shape of `RunBuild` / `RunVet` from `checks.go`.

- [ ] **Step 1: Write the failing no-op test**

We can test the no-op path (nothing detected → skipped CheckResult) in unit tests. The "run real make or golangci-lint" paths are covered by the manual smoke test in Task 8. Append to `internal/codereviewer/linter_test.go`:

```go
func TestRunLint_NoConfigIsNoop(t *testing.T) {
	dir := t.TempDir()
	// Stub lookPathFn so no binary is considered present.
	t.Cleanup(func() { lookPathFn = exec.LookPath })
	lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

	res, err := RunLint(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunLint error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true for no-op, got false")
	}
	if res.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "no configuration detected") {
		t.Errorf("expected no-op sentinel in output, got %q", res.Output)
	}
}

func TestRunLint_AgentLintOff(t *testing.T) {
	dir := t.TempDir()
	// Even with a real lint target, AGENT_LINT=off wins.
	mustWrite(t, filepath.Join(dir, "Makefile"), "lint:\n\techo should-not-run\n")
	t.Setenv("AGENT_LINT", "off")

	res, err := RunLint(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunLint error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true when disabled, got false")
	}
	if !strings.Contains(res.Output, "no configuration detected") {
		t.Errorf("expected no-op sentinel, got %q", res.Output)
	}
}
```

Add `"context"` to `linter_test.go` imports if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/codereviewer/... -run TestRunLint -v`
Expected: FAIL with `undefined: RunLint`.

- [ ] **Step 3: Implement `RunLint` in `linter.go`**

Append to `internal/codereviewer/linter.go`:

```go
// Add to imports: "context"

// RunLint runs the target repo's configured linter against repoDir and
// returns a deterministic CheckResult, using the same bounded-env /
// capped-output pattern as runGo in checks.go.
//
// When detectLinter returns nil (no lint workflow found or disabled),
// RunLint returns Passed: true with a sentinel output string so that
// Review() can distinguish "lint ran and passed" from "lint was not
// configured" without needing a second return value.
func RunLint(ctx context.Context, repoDir string) (*CheckResult, error) {
	cfg, err := detectLinter(repoDir)
	if err != nil {
		return nil, fmt.Errorf("detecting linter: %w", err)
	}
	if cfg == nil {
		return &CheckResult{
			Passed:   true,
			ExitCode: 0,
			Output:   "lint: no configuration detected, skipped",
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch cfg.Mode {
	case "make":
		cmd = exec.CommandContext(ctx, "make", "lint")
	case "golangci-lint":
		cmd = exec.CommandContext(ctx, "golangci-lint", "run", "--out-format=line-number", "./...")
	default:
		return nil, fmt.Errorf("unknown lint mode %q", cfg.Mode)
	}
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"GOROOT=" + os.Getenv("GOROOT"),
		"TMPDIR=" + os.TempDir(),
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
	}

	stdout := &cappedBuffer{cap: maxCheckOutput}
	stderr := &cappedBuffer{cap: maxCheckOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%s lint timed out after %s", cfg.Mode, checkTimeout)
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("running %s lint: %w", cfg.Mode, runErr)
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	wasTruncated := stdout.Truncated() || stderr.Truncated() || len(output) > maxCheckOutput
	if len(output) > maxCheckOutput {
		output = output[:maxCheckOutput]
	}
	if wasTruncated {
		output += "\n... (truncated)"
	}

	return &CheckResult{
		Passed:   exitCode == 0,
		ExitCode: exitCode,
		Output:   output,
	}, nil
}
```

Add `"errors"` to `linter.go` imports alongside the earlier additions.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/codereviewer/... -run TestRunLint -v`
Expected: both subtests PASS.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/codereviewer/... -short`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/codereviewer/linter.go internal/codereviewer/linter_test.go
git commit -m "feat(codereviewer): add RunLint check (#34)

Wraps detectLinter + bounded command execution behind a single
entry point. Reuses the checks.go env/timeout/truncation pattern
so lint shares the same security posture as go build/go vet. No
callers yet — Review() wires it in the next commit."
```

---

## Task 6: Extract `collectChangedFiles` helper

**Files:**
- Modify: `internal/codereviewer/reviewer.go`

Pure refactor. `Review()` needs the changed-file set available before lint runs, so we split the existing `collectDiff` into a standalone `collectChangedFiles` helper and thin the diff-collection step to return only the diff. No behavior change.

- [ ] **Step 1: Read the existing `collectDiff` function**

Open `internal/codereviewer/reviewer.go` lines 215-257 so the next step is unambiguous.

- [ ] **Step 2: Replace `collectDiff` with the helper split**

Replace the existing `collectDiff` function (starting at "// collectDiff runs `git add -N .` ..." and ending at the closing brace) with:

```go
// collectChangedFiles stages untracked files via `git add -N .` and
// returns the list of touched files parsed from `git status --porcelain`.
// Runs under a shared gitTimeout so a stuck git index or hung subprocess
// cannot block the pipeline indefinitely.
//
// `git add -N .` only records intent-to-add — it does not modify file
// contents — so calling this multiple times is safe and idempotent.
func collectChangedFiles(ctx context.Context, repoDir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	addCmd := exec.CommandContext(ctx, "git", "add", "-N", ".")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add -N: %w\n%s", err, out)
	}

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
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
	return files, nil
}

// collectDiff runs `git diff HEAD` in repoDir and returns the unified
// diff, truncated to maxDiffBytes. Callers must invoke collectChangedFiles
// first (within the same Review() pass) so untracked files are staged
// with `git add -N .` before the diff runs — otherwise new files would
// be silently omitted from the diff passed to the semantic reviewer.
func collectDiff(ctx context.Context, repoDir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	diffCmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diffCmd.Dir = repoDir
	var diffOut bytes.Buffer
	diffCmd.Stdout = &diffOut
	var diffStderr bytes.Buffer
	diffCmd.Stderr = &diffStderr
	if err := diffCmd.Run(); err != nil {
		return "", fmt.Errorf("git diff HEAD: %w\n%s", err, diffStderr.String())
	}
	diff := diffOut.String()
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n... (diff truncated)"
	}
	return diff, nil
}
```

- [ ] **Step 3: Update the `Review()` caller to use the new two-function API**

In `Review()`, find the block starting at "// 3. Collect the diff" (around line 178). Replace:

```go
	// 3. Collect the diff (including untracked files via `git add -N .`).
	diff, files, err := collectDiff(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting diff: %w", err)
	}
```

with:

```go
	// 3. Collect the changed-file set (also stages untracked files via
	// `git add -N .` so the subsequent diff sees them).
	files, err := collectChangedFiles(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting changed files: %w", err)
	}

	// 4. Collect the unified diff for the semantic reviewer.
	diff, err := collectDiff(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting diff: %w", err)
	}
```

Renumber the comments on the subsequent steps if they still say "4. Semantic LLM review" — shift to "5.".

- [ ] **Step 4: Verify compilation and existing tests**

Run: `go build ./internal/codereviewer/...`
Expected: clean build.

Run: `go test ./internal/codereviewer/... -short`
Expected: all tests pass — this is a pure refactor.

- [ ] **Step 5: Commit**

```bash
git add internal/codereviewer/reviewer.go
git commit -m "refactor(codereviewer): split collectDiff into collectChangedFiles + collectDiff (#34)

The upcoming lint step needs the changed-file set before the diff
runs (so it can filter lint output by changed files), so split the
existing collectDiff into a dedicated changed-file helper and a
diff-only helper. Behavior unchanged — git add -N . still runs
exactly once per Review() pass because Review() now calls both
helpers in sequence."
```

---

## Task 7: Wire `RunLint` into `Review()` with integration tests

**Files:**
- Modify: `internal/codereviewer/reviewer.go`
- Modify: `internal/codereviewer/reviewer_test.go`

Where everything gets stitched together. `Review()` gains a lint step between vet and semantic review; a `runLintFn` package var lets integration tests stub the check without spawning real processes.

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/codereviewer/reviewer_test.go`:

```go
// stubReviewSemantics is a helper that replaces reviewSemantics for the
// duration of a test and restores it on cleanup. Returns a pointer to
// the call counter so tests can assert whether the semantic call was
// reached.
func stubReviewSemantics(t *testing.T, verdict *llmVerdict) *int {
	t.Helper()
	calls := 0
	orig := reviewSemantics
	reviewSemantics = func(ctx context.Context, apiKey, prompt string) (*llmVerdict, int, int, error) {
		calls++
		return verdict, 10, 5, nil
	}
	t.Cleanup(func() { reviewSemantics = orig })
	return &calls
}

// stubRunLint replaces runLintFn for the duration of a test.
func stubRunLint(t *testing.T, res *CheckResult, err error) {
	t.Helper()
	orig := runLintFn
	runLintFn = func(ctx context.Context, repoDir string) (*CheckResult, error) {
		return res, err
	}
	t.Cleanup(func() { runLintFn = orig })
}

// newReviewableRepo builds a git repo with a trivial main.go commit
// plus a dirty edit, so collectChangedFiles / collectDiff have something
// to return. Used by the lint-wiring tests below.
func newReviewableRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	gitInit(t, dir)
	gitCommitAll(t, dir, "initial")
	// Dirty edit so collectChangedFiles returns main.go.
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() { _ = 1 }\n")
	return dir
}

func TestReview_LintPassesThenSemanticRuns(t *testing.T) {
	dir := newReviewableRepo(t)
	stubRunLint(t, &CheckResult{Passed: true, Output: "lint: ok"}, nil)
	calls := stubReviewSemantics(t, &llmVerdict{Approved: true, Feedback: "looks good"})

	verdict, err := Review(context.Background(), "dummy-key", dir,
		&github.Issue{Number: 1, Title: "t", Body: "t"},
		&planner.ImplementationPlan{Markdown: "plan"},
		&archivist.Dossier{Summary: "summary"},
	)
	if err != nil {
		t.Fatalf("Review error: %v", err)
	}
	if !verdict.Approved {
		t.Errorf("expected Approved=true, got %+v", verdict)
	}
	if verdict.LintOutput != "lint: ok" {
		t.Errorf("expected LintOutput='lint: ok', got %q", verdict.LintOutput)
	}
	if *calls != 1 {
		t.Errorf("expected 1 semantic call, got %d", *calls)
	}
}

func TestReview_LintFailsInChangedFiles(t *testing.T) {
	dir := newReviewableRepo(t)
	// main.go is the only changed file; report an error on it.
	lintOut := "main.go:1:1: exported function main should have comment (golint)\n"
	stubRunLint(t, &CheckResult{Passed: false, ExitCode: 1, Output: lintOut}, nil)
	calls := stubReviewSemantics(t, &llmVerdict{Approved: true, Feedback: "unreachable"})

	verdict, err := Review(context.Background(), "dummy-key", dir,
		&github.Issue{Number: 1, Title: "t", Body: "t"},
		&planner.ImplementationPlan{Markdown: "plan"},
		&archivist.Dossier{Summary: "summary"},
	)
	if err != nil {
		t.Fatalf("Review error: %v", err)
	}
	if verdict.Approved {
		t.Errorf("expected Approved=false, got %+v", verdict)
	}
	if verdict.Category != "lint" {
		t.Errorf("expected Category=lint, got %q", verdict.Category)
	}
	if verdict.LintErrorsKept != 1 {
		t.Errorf("expected LintErrorsKept=1, got %d", verdict.LintErrorsKept)
	}
	if verdict.LintErrorsDropped != 0 {
		t.Errorf("expected LintErrorsDropped=0, got %d", verdict.LintErrorsDropped)
	}
	if !strings.Contains(verdict.Feedback, "## Lint Errors") {
		t.Errorf("expected feedback to contain lint header, got %q", verdict.Feedback)
	}
	if *calls != 0 {
		t.Errorf("expected 0 semantic calls (short circuit), got %d", *calls)
	}
}

func TestReview_LintFailsOnlyInUnchangedFiles_AdvisoryPass(t *testing.T) {
	dir := newReviewableRepo(t)
	// Error in a file that is NOT in the changed set — pre-existing debt.
	lintOut := "unchanged/pre_existing.go:42:5: some warning (stylecheck)\n"
	stubRunLint(t, &CheckResult{Passed: false, ExitCode: 1, Output: lintOut}, nil)
	calls := stubReviewSemantics(t, &llmVerdict{Approved: true, Feedback: "looks good"})

	verdict, err := Review(context.Background(), "dummy-key", dir,
		&github.Issue{Number: 1, Title: "t", Body: "t"},
		&planner.ImplementationPlan{Markdown: "plan"},
		&archivist.Dossier{Summary: "summary"},
	)
	if err != nil {
		t.Fatalf("Review error: %v", err)
	}
	if !verdict.Approved {
		t.Errorf("expected Approved=true (advisory pass), got %+v", verdict)
	}
	if verdict.LintErrorsKept != 0 {
		t.Errorf("expected LintErrorsKept=0, got %d", verdict.LintErrorsKept)
	}
	if verdict.LintErrorsDropped != 1 {
		t.Errorf("expected LintErrorsDropped=1, got %d", verdict.LintErrorsDropped)
	}
	if *calls != 1 {
		t.Errorf("expected 1 semantic call, got %d", *calls)
	}
}

func TestReview_LintDetectionReturnsNil_SilentSkip(t *testing.T) {
	dir := newReviewableRepo(t)
	// RunLint returns the no-op sentinel CheckResult.
	stubRunLint(t, &CheckResult{Passed: true, Output: "lint: no configuration detected, skipped"}, nil)
	calls := stubReviewSemantics(t, &llmVerdict{Approved: true, Feedback: "ok"})

	verdict, err := Review(context.Background(), "dummy-key", dir,
		&github.Issue{Number: 1, Title: "t", Body: "t"},
		&planner.ImplementationPlan{Markdown: "plan"},
		&archivist.Dossier{Summary: "summary"},
	)
	if err != nil {
		t.Fatalf("Review error: %v", err)
	}
	if !verdict.Approved {
		t.Errorf("expected Approved=true, got %+v", verdict)
	}
	if !strings.Contains(verdict.LintOutput, "no configuration detected") {
		t.Errorf("expected LintOutput to contain skip sentinel, got %q", verdict.LintOutput)
	}
	if verdict.LintErrorsKept != 0 || verdict.LintErrorsDropped != 0 {
		t.Errorf("expected zero lint counters on skip, got kept=%d dropped=%d",
			verdict.LintErrorsKept, verdict.LintErrorsDropped)
	}
	if *calls != 1 {
		t.Errorf("expected 1 semantic call, got %d", *calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/codereviewer/... -run TestReview_Lint -v`
Expected: FAIL — either `undefined: runLintFn` or the tests run but assertions fail because `Review()` doesn't touch lint fields yet.

- [ ] **Step 3: Add `runLintFn` package var and wire lint into `Review()`**

In `internal/codereviewer/reviewer.go`, alongside the existing `reviewSemantics` package var (around line 83-84), add:

```go
// runLintFn is a package var so tests can replace it with a stub.
// Default is the real RunLint implementation in linter.go.
var runLintFn = RunLint
```

Then in `Review()`, after the vet block and before the existing "// 3. Collect the changed-file set" section (which Task 6 renamed), insert the new lint step. The result should look like:

```go
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

	// 3. Collect the changed-file set (also stages untracked files via
	// `git add -N .` so the subsequent diff sees them).
	files, err := collectChangedFiles(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting changed files: %w", err)
	}

	// 4. Target-repo linter. Filters errors by changed files so
	// pre-existing lint debt in the target repo cannot block the
	// pipeline on code we did not touch.
	lint, err := runLintFn(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("running lint: %w", err)
	}
	verdict.LintOutput = lint.Output
	if !lint.Passed {
		kept, dropped := filterLintErrors(lint.Output, files)
		verdict.LintErrorsKept = len(kept)
		verdict.LintErrorsDropped = dropped
		if len(kept) > 0 {
			verdict.Approved = false
			verdict.Category = "lint"
			verdict.Summary = fmt.Sprintf("%d lint error(s) in changed files", len(kept))
			verdict.Feedback = formatLintFeedback(kept)
			return verdict, nil
		}
		// Advisory pass — all reported errors are in unchanged files
		// and therefore pre-existing debt we should not retry on.
	}

	// 5. Collect the unified diff for the semantic reviewer.
	diff, err := collectDiff(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting diff: %w", err)
	}

	// 6. Semantic LLM review.
	prompt := buildReviewPrompt(issue, plan, dossier, diff, files)
	llmResult, inTokens, outTokens, err := reviewSemantics(ctx, geminiKey, prompt)
	if err != nil {
		return nil, fmt.Errorf("semantic reviewer: %w", err)
	}
```

Keep the rest of the function (token recording, approval logic) unchanged. The `// 3.` through `// 6.` comments are updated to match the new step order. This is the only behavior change outside of the lint insertion.

Note: the lint-failed-but-all-dropped path falls through to the diff/semantic block. The LintErrorsKept/Dropped counters are already populated on the verdict, so they survive on the approval path.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/codereviewer/... -run TestReview_Lint -v`
Expected: all 4 subtests PASS.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/codereviewer/... -short`
Expected: all tests pass, including prior `TestReview_ShortCircuitsOnBuildFailure` and `TestReview_ShortCircuitsOnVetFailure`. Those tests don't stub `runLintFn`, but they short-circuit before lint runs (on build/vet failure) so they should be unaffected.

Note: If the short-circuit tests now fail because the default `runLintFn = RunLint` is being called before they return, that would indicate the lint step was inserted in the wrong position (it must come *after* the vet block). Re-verify the insertion point and the early returns in the build/vet blocks.

- [ ] **Step 6: Verify the whole repo still builds**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/codereviewer/reviewer.go internal/codereviewer/reviewer_test.go
git commit -m "feat(codereviewer): wire RunLint into Review() with changed-file filtering (#34)

Adds the lint gate between vet and the semantic reviewer. Failures
with at least one error in the changed-file set reject the verdict
with Category=lint and formatted feedback for the retry loop.
Failures whose errors all live in unchanged files are treated as an
advisory pass (pre-existing debt), letting the semantic reviewer
continue unobstructed. runLintFn is stubbable for tests."
```

---

## Task 8: Logging + manual smoke test

**Files:**
- Modify: `internal/codereviewer/reviewer.go`
- Test: manual smoke test against ConduitIO/conduit (no file changes)

- [ ] **Step 1: Add lint log lines to `Review()`**

In `Review()`, just before `lint, err := runLintFn(...)`, add:

```go
	log.Printf("Running target-repo linter...")
```

And just after the `if !lint.Passed { ... }` block (whether it returned a rejection or fell through as an advisory pass), add a single log line that covers both outcomes. Place it at the end of the lint block, after the comment that says "Advisory pass — ...":

```go
	log.Printf("Lint check: passed=%v kept=%d dropped=%d", lint.Passed, verdict.LintErrorsKept, verdict.LintErrorsDropped)
```

Note: this line needs to run on *both* the passed path and the advisory-pass fall-through path. Since the rejection path returns early at `return verdict, nil` inside the `if len(kept) > 0` branch, moving the log line to after the entire lint block (just before the `collectDiff` call) means it fires in exactly the two cases we want — passed or advisory-pass — and is skipped on hard rejection (which already has its own `Category: "lint"` verdict to log at the caller).

Add `"log"` to the `reviewer.go` import block — the file does not currently import `log` (verified against the pre-plan state of the file), so this is a required addition, not a double-check.

- [ ] **Step 2: Verify compilation and tests**

Run: `go build ./internal/codereviewer/...`
Expected: clean build.

Run: `go test ./internal/codereviewer/... -short`
Expected: all tests pass. The log lines go to stderr and do not affect test assertions.

- [ ] **Step 3: Commit the log lines**

```bash
git add internal/codereviewer/reviewer.go
git commit -m "feat(codereviewer): log lint check outcome in Review (#34)

Mirrors the existing build/vet logging so operators can see at a
glance whether the lint gate ran, passed, rejected, or advisory-
passed for any given run."
```

- [ ] **Step 4: Run the manual smoke test against ConduitIO/conduit**

This step is **not** automatable in CI — it requires a real clone, real `make lint`, and a real run through the implementer pipeline. Before declaring the feature done, perform and record the results of each smoke case below. If any case fails, stop and fix before proceeding to the PR.

**Smoke case A — real `make lint` runs cleanly on an unmodified clone:**

```bash
# Throwaway workdir
cd $(mktemp -d)
git clone --depth=50 https://github.com/ConduitIO/conduit.git
cd conduit
# Sanity check: what does the repo's lint target look like?
grep -A3 '^lint:' Makefile
# Run Review() against an empty working tree via a small Go harness, or
# simply invoke make lint directly to confirm it exits 0 in a clean tree.
make lint
```

Expected: `make lint` exits 0 on unmodified main. If it does not, seed a `data/runs/*/run-summary.json` note that this repo currently has pre-existing debt — the advisory-pass path should handle it.

**Smoke case B — a seeded lint error in a changed file is caught:**

Run the full implementer pipeline end-to-end against any ConduitIO issue using a local branch where you have manually introduced a deliberate unused variable. Example: pick an existing issue number, run through `cmd/implementer/main.go`, and in the implementer's working copy append an unused variable to the first file it modifies.

```bash
AGENT_LINT=on \
GOOGLE_API_KEY=... \
ANTHROPIC_API_KEY=... \
GITHUB_TOKEN=... \
go run ./cmd/implementer -issue <issue-number>
```

Expected in logs:
- `Running target-repo linter...`
- `Lint check: passed=false kept=1 dropped=...` (or whatever count)
- `Code review verdict: approved=false category="lint" ...`
- `Retrying implementer with reviewer feedback...`
- Retry either fixes the error (pipeline proceeds to PR) or halts with `halting before PR creation — code review failed twice`.
- `code-review.json` in the run artifacts contains the new `lint_output`, `lint_errors_kept`, and `lint_errors_dropped` fields.

**Smoke case C — `AGENT_LINT=off` short-circuits:**

```bash
AGENT_LINT=off \
GOOGLE_API_KEY=... \
ANTHROPIC_API_KEY=... \
GITHUB_TOKEN=... \
go run ./cmd/implementer -issue <issue-number>
```

Expected in logs:
- `Running target-repo linter...`
- `Lint check: passed=true kept=0 dropped=0` (the no-op sentinel path)
- `code-review.json` contains `"lint_output": "lint: no configuration detected, skipped"` (because `detectLinter` returned nil under `AGENT_LINT=off`).

- [ ] **Step 5: Record smoke test results**

If all three cases pass, move on to opening the PR via the project's normal flow. If any case fails, create a new follow-up task in this plan document (append below) describing the failure and the fix before marking the plan complete.

---

## Verification checklist

Before declaring the plan complete, confirm:

- [ ] `go build ./...` is clean
- [ ] `go test ./internal/codereviewer/... -short -v` passes all subtests (old + new)
- [ ] `go test ./...` at repo root is no worse than it was before the plan started (project-level flakiness is tracked separately; do not chase unrelated failures)
- [ ] `code-review.json` artifact from a real run contains the new `lint_output`, `lint_errors_kept`, and `lint_errors_dropped` fields
- [ ] Manual smoke cases A, B, and C all produced their expected log output
- [ ] No new files under `internal/linter/` — lint lives inside `internal/codereviewer` by design
- [ ] No changes to `cmd/implementer/main.go`'s retry block — the retry loop consumes the existing `verdict.Feedback` wiring unchanged
- [ ] Commit history is linear: Task 1, Task 2, ..., Task 8 as separate commits
