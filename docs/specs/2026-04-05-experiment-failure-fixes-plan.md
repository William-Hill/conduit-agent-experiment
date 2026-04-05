# Experiment Failure Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the five distinct failure modes surfaced by experiments 01-03, so the next batch of runs produces meaningful signal.

**Architecture:** Each fix is a targeted change to the existing pipeline. Fixes are ordered by dependency: artifact capture first (unblocks accurate review), baseline verifier second (enables environmental classification), then cross-file consistency, verifier allowlist expansion, and finally the revision loop (which benefits from all earlier fixes being in place). All changes are backwards-compatible; existing run artifacts and task definitions remain valid.

**Tech Stack:** Go 1.24, existing internal packages (`orchestrator`, `agents`, `execution`, `models`, `retrieval`), no new dependencies.

**Source:** Experiment docs at `docs/experiments/` — experiments 01, 02, 03.

---

## File Structure

| File | Changes | Responsible For |
|------|---------|-----------------|
| `internal/models/run.go` | Add `NewFiles`, `BaselineCommands`, `Revisions` fields | Run artifact model |
| `internal/agents/architect.go` | Add `NewFiles` to `ArchitectInput`, update prompt | Architect review input |
| `internal/agents/implementer.go` | Add `siblingContents` param to `GenerateFileContent`, update prompt | Cross-file context |
| `internal/agents/verifier.go` | Expand allowlist, add `VerifyBaseline()`, add `ClassifyResults()` | Verification + baseline |
| `internal/orchestrator/workflow.go` | Baseline capture, new-file capture, sibling accumulation, revision loop | Pipeline orchestration |
| `internal/retrieval/dossier_builder.go` | Detect non-Go files for verifier commands | Dossier command selection |
| `internal/config/config.go` | Add `MaxRevisions` to `PolicyConfig` | Revision loop config |
| `configs/experiment.yaml` | Add `max_revisions: 1` | Default config |

Tests follow the same structure — each task has tests before implementation.

---

## Task 1: Capture Newly Created Files in Run Artifacts

**Problem:** `git diff` only shows tracked files. New files from `plan.FilesToCreate` are invisible in artifacts and architect review (experiment 03).

**Files:**
- Modify: `internal/models/run.go:43`
- Modify: `internal/orchestrator/workflow.go:269-271, 283-290, 356`
- Modify: `internal/agents/architect.go:41-48, 81-146`
- Test: `internal/orchestrator/workflow_test.go`
- Test: `internal/agents/architect_test.go`

- [ ] **Step 1: Add `NewFiles` field to Run model**

```go
// In internal/models/run.go, add after ImplementerDiff (line 43):
NewFiles map[string]string `json:"new_files,omitempty"` // path -> content for newly created files
```

- [ ] **Step 2: Add `NewFiles` to `ArchitectInput`**

```go
// In internal/agents/architect.go, add to ArchitectInput struct (after FailedFiles):
NewFiles map[string]string // path -> content of newly created files (not in git diff)
```

- [ ] **Step 3: Update `buildArchitectPrompt` to include new files**

In `internal/agents/architect.go`, add a new section in `buildArchitectPrompt` after the diff section (after line 126):

```go
// New files not in the diff (untracked).
if len(input.NewFiles) > 0 {
	fmt.Fprintf(&b, "## Newly Created Files\n")
	fmt.Fprintf(&b, "These files were created by the implementer but are not in the diff (untracked by git).\n\n")
	for path, content := range input.NewFiles {
		fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", path, content)
	}
}
```

- [ ] **Step 4: Capture new files in workflow.go**

In `internal/orchestrator/workflow.go`, after the `git diff` capture (line 270) and before the verifier call (line 274), add:

```go
// --- 8b. Capture newly created files (not in git diff) ---
newFiles := make(map[string]string)
for _, fc := range plan.FilesToCreate {
	fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
	if pathErr != nil {
		continue
	}
	data, readErr := os.ReadFile(fullPath)
	if readErr != nil {
		continue
	}
	newFiles[fc.Path] = string(data)
}
```

Then pass `newFiles` to `architectInput` (around line 290):

```go
architectInput := agents.ArchitectInput{
	Diff:             diff,
	Dossier:          dossier,
	Plan:             plan,
	VerifierReport:   verifierReport,
	SupplementalDocs: supplementalDocs,
	FailedFiles:      failedFiles,
	NewFiles:         newFiles,
}
```

And store in the run (around line 356):

```go
run.ImplementerDiff = diff
run.NewFiles = newFiles
```

- [ ] **Step 5: Write test for new-file capture**

In `internal/orchestrator/workflow_test.go`, add a test case for `TestRunWorkflow` (or extend the existing integration test) that:
- Uses a mock implementer that creates a new file via `FilesToCreate`
- Verifies `result.Run.NewFiles` contains the created file's content
- Verifies the architect input includes the new file

- [ ] **Step 6: Write test for architect prompt with new files**

In `internal/agents/architect_test.go`, add:

```go
func TestBuildArchitectPromptNewFiles(t *testing.T) {
	input := ArchitectInput{
		Diff:    "--- a/foo.go\n+++ b/foo.go",
		Dossier: models.Dossier{TaskID: "task-001"},
		Plan:    PatchPlan{PlanSummary: "some plan"},
		VerifierReport: VerifierReport{OverallPass: true, Summary: "pass"},
		NewFiles: map[string]string{
			"scripts/update.sh": "#!/bin/bash\necho hello",
		},
	}
	prompt := buildArchitectPrompt(input)
	if !strings.Contains(prompt, "Newly Created Files") {
		t.Error("prompt should contain Newly Created Files section")
	}
	if !strings.Contains(prompt, "scripts/update.sh") {
		t.Error("prompt should contain new file path")
	}
	if !strings.Contains(prompt, "echo hello") {
		t.Error("prompt should contain new file content")
	}
}
```

- [ ] **Step 7: Run tests**

```bash
go test ./internal/agents/ ./internal/orchestrator/ -v -run "NewFiles|Architect"
```

Expected: all new tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/models/run.go internal/agents/architect.go internal/agents/architect_test.go internal/orchestrator/workflow.go internal/orchestrator/workflow_test.go
git commit -m "feat: capture newly created files in run artifacts and architect review

New files from plan.FilesToCreate are now read from the worktree after
generation and stored in Run.NewFiles. The architect prompt includes
their full content so newly created scripts/configs can be reviewed."
```

---

## Task 2: Baseline Verifier State

**Problem:** Environmental failures (pre-existing test failures, go vet issues) get attributed to the patch because the verifier has no pre-patch baseline (experiments 01, 03).

**Files:**
- Modify: `internal/agents/verifier.go:20-85`
- Modify: `internal/orchestrator/workflow.go:119-128, 273-275`
- Modify: `internal/models/run.go`
- Test: `internal/agents/verifier_test.go`

- [ ] **Step 1: Add baseline fields to Run model**

```go
// In internal/models/run.go, add:
BaselineCommands []CommandLog `json:"baseline_commands,omitempty"`
```

- [ ] **Step 2: Add `VerifyBaseline` function**

In `internal/agents/verifier.go`, add a new function that runs the same commands as `Verify` but returns results without the "overall pass" judgment:

```go
// VerifyBaseline runs the same commands as Verify on an unpatched worktree to
// establish which commands already fail before the patch is applied.
func VerifyBaseline(ctx context.Context, runner *execution.CommandRunner, dossier models.Dossier) []models.CommandLog {
	var logs []models.CommandLog
	for _, cmd := range dossier.LikelyCommands {
		if !isAllowedCommand(cmd) {
			continue
		}
		log := runner.Run(ctx, cmd)
		logs = append(logs, log)
	}
	return logs
}
```

- [ ] **Step 3: Add `ClassifyResults` function**

In `internal/agents/verifier.go`, add a function that compares baseline vs post-patch:

```go
// ClassifyResults compares baseline and post-patch command results.
// A command that fails in both baseline and post-patch is classified as
// environmental. A command that passes in baseline but fails post-patch
// is classified as patch-caused.
func ClassifyResults(baseline, postPatch []models.CommandLog) (patchFailures, envFailures []string) {
	baselineStatus := make(map[string]int) // command -> exit code
	for _, bl := range baseline {
		baselineStatus[bl.Command] = bl.ExitCode
	}
	for _, pp := range postPatch {
		if pp.ExitCode != 0 {
			if blCode, ok := baselineStatus[pp.Command]; ok && blCode != 0 {
				envFailures = append(envFailures, pp.Command)
			} else {
				patchFailures = append(patchFailures, pp.Command)
			}
		}
	}
	return
}
```

- [ ] **Step 4: Wire baseline into workflow.go**

In `internal/orchestrator/workflow.go`, after worktree setup (line ~128) but before the implementer writes files (line ~165), add:

```go
// --- 5b. Baseline verifier ---
baselineLogs := agents.VerifyBaseline(ctx, runner, dossier)
```

Then after the existing `Verify` call (line ~274), add classification:

```go
patchFailures, envFailures := agents.ClassifyResults(baselineLogs, verifierReport.CommandLogs)
verifierReport.PatchFailures = patchFailures
verifierReport.EnvironmentFailures = envFailures
```

Store baseline in the run:

```go
run.BaselineCommands = baselineLogs
```

- [ ] **Step 5: Add classification fields to VerifierReport**

In `internal/agents/verifier.go`, extend `VerifierReport`:

```go
type VerifierReport struct {
	OverallPass         bool              `json:"overall_pass"`
	Summary             string            `json:"summary"`
	CommandLogs         []models.CommandLog `json:"command_logs"`
	PatchFailures       []string          `json:"patch_failures,omitempty"`
	EnvironmentFailures []string          `json:"environment_failures,omitempty"`
}
```

Update `OverallPass` logic: only `PatchFailures` count against pass (not env failures).

- [ ] **Step 6: Update architect prompt to include classification**

In `internal/agents/architect.go`, update the verification section in `buildArchitectPrompt` (around line 121-122):

```go
fmt.Fprintf(&b, "Overall: %s\n", pass)
fmt.Fprintf(&b, "Summary: %s\n", input.VerifierReport.Summary)
if len(input.VerifierReport.EnvironmentFailures) > 0 {
	fmt.Fprintf(&b, "\nNote: the following commands failed in both the baseline (before patch) and post-patch runs, indicating pre-existing environment issues rather than patch-caused failures:\n")
	for _, cmd := range input.VerifierReport.EnvironmentFailures {
		fmt.Fprintf(&b, "- %s (environment)\n", cmd)
	}
}
if len(input.VerifierReport.PatchFailures) > 0 {
	fmt.Fprintf(&b, "\nThe following commands passed before the patch but fail after, indicating patch-caused failures:\n")
	for _, cmd := range input.VerifierReport.PatchFailures {
		fmt.Fprintf(&b, "- %s (patch-caused)\n", cmd)
	}
}
```

- [ ] **Step 7: Write test for ClassifyResults**

In `internal/agents/verifier_test.go`:

```go
func TestClassifyResults(t *testing.T) {
	baseline := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
		{Command: "go vet ./...", ExitCode: 1}, // pre-existing failure
	}
	postPatch := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 1}, // new failure from patch
		{Command: "go vet ./...", ExitCode: 1},   // same as baseline
	}
	patchFails, envFails := ClassifyResults(baseline, postPatch)
	if len(patchFails) != 1 || patchFails[0] != "go build ./..." {
		t.Errorf("patch failures = %v, want [go build ./...]", patchFails)
	}
	if len(envFails) != 1 || envFails[0] != "go vet ./..." {
		t.Errorf("env failures = %v, want [go vet ./...]", envFails)
	}
}

func TestClassifyResultsAllPass(t *testing.T) {
	baseline := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
	}
	postPatch := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
	}
	patchFails, envFails := ClassifyResults(baseline, postPatch)
	if len(patchFails) != 0 {
		t.Errorf("expected no patch failures, got %v", patchFails)
	}
	if len(envFails) != 0 {
		t.Errorf("expected no env failures, got %v", envFails)
	}
}
```

- [ ] **Step 8: Run tests**

```bash
go test ./internal/agents/ ./internal/orchestrator/ -v -run "Classify|Baseline"
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/agents/verifier.go internal/agents/verifier_test.go internal/orchestrator/workflow.go internal/models/run.go internal/agents/architect.go
git commit -m "feat: add baseline verifier to classify environmental vs patch-caused failures

Runs verifier commands before the implementer writes files to establish
baseline. Post-patch failures that also failed at baseline are classified
as environment_setup_failure rather than patch failures. Architect prompt
explicitly labels each failure class."
```

---

## Task 3: Expand Verifier Allowlist for Non-Go Commands

**Problem:** The verifier only permits Go toolchain commands. CI/YAML/shell tasks have no validation (experiment 03).

**Files:**
- Modify: `internal/agents/verifier.go:20-27`
- Test: `internal/agents/verifier_test.go`

- [ ] **Step 1: Write test for new allowed commands**

In `internal/agents/verifier_test.go`:

```go
func TestIsAllowedCommand_NonGo(t *testing.T) {
	cases := []struct {
		cmd     string
		allowed bool
	}{
		{"shellcheck scripts/update.sh", true},
		{"yamllint .github/workflows/release.yml", true},
		{"actionlint", true},
		{"test -f scripts/update.sh", true},
		{"cat scripts/update.sh", true},
		{"rm -rf /", false},
		{"curl evil.com", false},
	}
	for _, tc := range cases {
		got := isAllowedCommand(tc.cmd)
		if got != tc.allowed {
			t.Errorf("isAllowedCommand(%q) = %v, want %v", tc.cmd, got, tc.allowed)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agents/ -v -run TestIsAllowedCommand_NonGo
```

Expected: FAIL for shellcheck, yamllint, actionlint, test, cat commands.

- [ ] **Step 3: Expand the allowlist**

In `internal/agents/verifier.go`, update `allowedCommandPrefixes` (line 20):

```go
var allowedCommandPrefixes = []string{
	"go test",
	"go vet",
	"go build",
	"make",
	"grep",
	"golangci-lint",
	"echo",
	"shellcheck",
	"yamllint",
	"actionlint",
	"test -f",
	"test -x",
	"cat",
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/agents/ -v -run TestIsAllowedCommand_NonGo
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agents/verifier.go internal/agents/verifier_test.go
git commit -m "feat: expand verifier allowlist for shell/YAML/CI validation

Adds shellcheck, yamllint, actionlint, test, and cat to the allowed
command prefixes so non-Go tasks can specify appropriate verifier
commands via the task's verifier_commands field."
```

---

## Task 4: Cross-File Naming Consistency

**Problem:** Per-file generation calls don't share state. When file A defines symbols that file B references, the independent LLM calls choose different names (experiment 02).

**Files:**
- Modify: `internal/agents/implementer.go:75-101, 150-162`
- Modify: `internal/orchestrator/workflow.go:169-234`
- Test: `internal/agents/implementer_test.go`

- [ ] **Step 1: Update `GenerateFileContent` signature**

In `internal/agents/implementer.go`, change the function signature (line 75):

```go
// GenerateFileContent generates the full content for a single file in the patch.
// siblingContents contains the already-generated content of other files in the
// same plan, keyed by path, so the LLM can maintain naming consistency.
func GenerateFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string) (string, models.LLMCall, error) {
```

- [ ] **Step 2: Update `buildFileContentPrompt` to include sibling context**

In `internal/agents/implementer.go`, update `buildFileContentPrompt` (around line 150) to accept and render sibling contents:

```go
func buildFileContentPrompt(plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Patch Plan\n%s\n\n", plan.PlanSummary)
	fmt.Fprintf(&b, "## Task\nTitle: %s\nDescription: %s\n\n", task.Title, task.Description)

	// Show already-generated sibling files for naming consistency.
	if len(siblingContents) > 0 {
		fmt.Fprintf(&b, "## Already Generated Files in This Plan\n")
		fmt.Fprintf(&b, "IMPORTANT: Use the exact names, types, and signatures defined in these files. Do not invent alternative names.\n\n")
		for path, content := range siblingContents {
			// Cap each sibling at 200 lines to control prompt size.
			lines := strings.Split(content, "\n")
			if len(lines) > 200 {
				content = strings.Join(lines[:200], "\n") + "\n// ... truncated"
			}
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", path, content)
		}
	}

	fmt.Fprintf(&b, "## Target File: %s\n", filePath)
	if currentContent != "" {
		fmt.Fprintf(&b, "### Current Content\n```\n%s\n```\n\n", currentContent)
	} else {
		fmt.Fprintf(&b, "This is a NEW file to be created.\n\n")
	}

	fmt.Fprintf(&b, "Return ONLY the complete file content. No markdown fences, no explanation, no preamble. Just the raw file content.")
	return b.String()
}
```

- [ ] **Step 3: Accumulate sibling contents in workflow.go**

In `internal/orchestrator/workflow.go`, in the per-file generation loops (lines 169-234), accumulate generated content:

Replace the existing FilesToChange loop (lines 169-210) with:

```go
generatedSiblings := make(map[string]string)

for _, fc := range plan.FilesToChange {
	fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
	if pathErr != nil {
		log.Printf("skipping file %s: %v", fc.Path, pathErr)
		failedFiles = append(failedFiles, fc.Path)
		continue
	}

	if fc.Action == "delete" {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			log.Printf("failed to delete %s: %v", fc.Path, err)
			failedFiles = append(failedFiles, fc.Path)
		}
		continue
	}

	currentContent := ""
	if cached, ok := fileContents[fc.Path]; ok {
		currentContent = cached
	} else {
		data, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			currentContent = string(data)
		}
	}

	newContent, genCall, err := agents.GenerateFileContent(ctx, implClient, implModel, plan, task, fc.Path, currentContent, generatedSiblings)
	if err != nil {
		log.Printf("implementer: generation failed for %s: %v — marking as failed, continuing", fc.Path, err)
		failedFiles = append(failedFiles, fc.Path)
		continue
	}
	llmCalls = append(llmCalls, genCall)
	generatedSiblings[fc.Path] = newContent

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("creating dir for %s: %w", fc.Path, err)
	}
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", fc.Path, err)
	}
}
```

Apply the same pattern to the FilesToCreate loop (lines 212-234):

```go
for _, fc := range plan.FilesToCreate {
	fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
	if pathErr != nil {
		log.Printf("skipping file %s: %v", fc.Path, pathErr)
		failedFiles = append(failedFiles, fc.Path)
		continue
	}

	newContent, genCall, err := agents.GenerateFileContent(ctx, implClient, implModel, plan, task, fc.Path, "", generatedSiblings)
	if err != nil {
		log.Printf("implementer: generation failed for %s: %v — marking as failed, continuing", fc.Path, err)
		failedFiles = append(failedFiles, fc.Path)
		continue
	}
	llmCalls = append(llmCalls, genCall)
	generatedSiblings[fc.Path] = newContent

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("creating dir for %s: %w", fc.Path, err)
	}
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", fc.Path, err)
	}
}
```

- [ ] **Step 4: Update existing GenerateFileContent tests**

In `internal/agents/implementer_test.go`, update any existing tests that call `GenerateFileContent` to pass an empty `siblingContents map[string]string{}` as the last argument.

- [ ] **Step 5: Write test for sibling context in prompt**

```go
func TestBuildFileContentPromptWithSiblings(t *testing.T) {
	plan := PatchPlan{PlanSummary: "Add error constants and use them"}
	task := models.Task{ID: "test", Title: "test task", Description: "test"}
	siblings := map[string]string{
		"pkg/errors.go": "package pkg\n\nvar ErrFoo = errors.New(\"foo\")\n",
	}
	prompt := buildFileContentPrompt(plan, task, "pkg/handler.go", "package pkg", siblings)
	if !strings.Contains(prompt, "Already Generated Files") {
		t.Error("prompt should contain sibling section")
	}
	if !strings.Contains(prompt, "ErrFoo") {
		t.Error("prompt should contain sibling symbol name")
	}
	if !strings.Contains(prompt, "Do not invent alternative names") {
		t.Error("prompt should contain consistency instruction")
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/agents/ ./internal/orchestrator/ -v
```

Expected: all pass (existing + new).

- [ ] **Step 7: Commit**

```bash
git add internal/agents/implementer.go internal/agents/implementer_test.go internal/orchestrator/workflow.go
git commit -m "feat: pass generated sibling file contents to per-file generation calls

Each GenerateFileContent call now receives a map of already-generated
sibling files so the LLM can maintain naming consistency across files.
The prompt includes sibling content with an instruction to reuse exact
names and signatures. Addresses the cross-file naming inconsistency
failure mode observed in experiment 02."
```

---

## Task 5: Architect-to-Implementer Revision Loop

**Problem:** When the architect recommends "revise", the pipeline records it and stops. No iteration. The architect catches real issues but the pipeline can't act on the feedback (all experiments).

**Files:**
- Modify: `internal/orchestrator/workflow.go:292-382`
- Modify: `internal/config/config.go`
- Modify: `internal/models/run.go`
- Modify: `configs/experiment.yaml`
- Test: `internal/orchestrator/workflow_test.go`

- [ ] **Step 1: Add `MaxRevisions` to config**

In `internal/config/config.go`, add to `PolicyConfig`:

```go
type PolicyConfig struct {
	MaxDifficulty    string `yaml:"max_difficulty"`
	MaxBlastRadius   string `yaml:"max_blast_radius"`
	AllowPush        bool   `yaml:"allow_push"`
	AllowMerge       bool   `yaml:"allow_merge"`
	RequireRationale bool   `yaml:"require_rationale"`
	MaxFilesChanged  int    `yaml:"max_files_changed"`
	MaxRevisions     int    `yaml:"max_revisions"`
}
```

In `configs/experiment.yaml`, add under `policy:`:

```yaml
  max_revisions: 1
```

- [ ] **Step 2: Add revision tracking to Run model**

In `internal/models/run.go`, add:

```go
Revisions int `json:"revisions,omitempty"`
```

- [ ] **Step 3: Extract post-implementer pipeline into a helper function**

The revision loop wraps the implementer-through-architect section. To avoid deeply nested code, extract the verify-and-review section into a helper.

In `internal/orchestrator/workflow.go`, create a new function:

```go
type verifyAndReviewResult struct {
	diff             string
	newFiles         map[string]string
	verifierReport   agents.VerifierReport
	architectReview  agents.ArchitectReviewResult
	llmCalls         []models.LLMCall
	baselineCommands []models.CommandLog
}

func verifyAndReview(
	ctx context.Context,
	runner *execution.CommandRunner,
	dossier models.Dossier,
	plan agents.PatchPlan,
	mcfg *config.ModelsConfig,
	failedFiles []string,
	baselineLogs []models.CommandLog,
) (*verifyAndReviewResult, error) {
	// git diff
	diffLog := runner.Run(ctx, "git diff")
	diff := diffLog.Stdout

	// Capture new files
	newFiles := make(map[string]string)
	for _, fc := range plan.FilesToCreate {
		fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
		if pathErr != nil {
			continue
		}
		data, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			continue
		}
		newFiles[fc.Path] = string(data)
	}

	// Verifier
	verifierReport := agents.Verify(ctx, runner, dossier)
	patchFailures, envFailures := agents.ClassifyResults(baselineLogs, verifierReport.CommandLogs)
	verifierReport.PatchFailures = patchFailures
	verifierReport.EnvironmentFailures = envFailures

	// Architect
	archModel := mcfg.ModelForRole("architect", "gemini-2.5-flash")
	archClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, archModel)

	supplementalDocs := findSupplementalDocs(runner.WorkDir, dossier)
	architectInput := agents.ArchitectInput{
		Diff:             diff,
		Dossier:          dossier,
		Plan:             plan,
		VerifierReport:   verifierReport,
		SupplementalDocs: supplementalDocs,
		FailedFiles:      failedFiles,
		NewFiles:         newFiles,
	}

	architectReview, archCalls, err := agents.ArchitectReview(ctx, archClient, archModel, architectInput)
	if err != nil {
		return nil, fmt.Errorf("architect review: %w", err)
	}

	return &verifyAndReviewResult{
		diff:            diff,
		newFiles:        newFiles,
		verifierReport:  verifierReport,
		architectReview: architectReview,
		llmCalls:        archCalls,
	}, nil
}
```

- [ ] **Step 4: Add revision prompt builder to implementer**

In `internal/agents/implementer.go`, add a function to build a revision-aware prompt:

```go
// ReviseFileContent re-generates a file incorporating architect feedback.
func ReviseFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, architectFeedback string) (string, models.LLMCall, error) {
	var b strings.Builder
	b.WriteString(buildFileContentPrompt(plan, task, filePath, currentContent, siblingContents))
	fmt.Fprintf(&b, "\n\n## Architect Revision Feedback\n")
	fmt.Fprintf(&b, "The architect reviewed the previous version of this file and requested revisions:\n\n%s\n\n", architectFeedback)
	fmt.Fprintf(&b, "Incorporate this feedback into your output. Return ONLY the complete revised file content.")

	response, call, err := callLLM(ctx, client, "implementer-revise", modelName, implementerFileSystemPrompt, b.String())
	if err != nil {
		return "", call, err
	}

	content := strings.TrimSpace(response)
	content = stripMarkdownFences(content)
	return content, call, nil
}
```

- [ ] **Step 5: Wire the revision loop in workflow.go**

In `internal/orchestrator/workflow.go`, replace the current post-implementer section (from git diff through final status) with a loop:

```go
// --- 8-10. Verify, architect, and optionally revise ---
maxRevisions := policy.MaxRevisions
if maxRevisions <= 0 {
	maxRevisions = 0 // no revision loop by default if unset
}

var lastResult *verifyAndReviewResult
revision := 0

for {
	result, err := verifyAndReview(ctx, runner, dossier, plan, mcfg, failedFiles, baselineLogs)
	if err != nil {
		return nil, err
	}
	llmCalls = append(llmCalls, result.llmCalls...)
	agentsInvoked = append(agentsInvoked, "verifier", "architect")
	lastResult = result

	// If architect approves or rejects, or we've exhausted revisions, stop.
	if result.architectReview.Recommendation != agents.RecommendRevise || revision >= maxRevisions {
		break
	}

	// --- Revision round ---
	revision++
	log.Printf("architect recommended revise (round %d/%d), re-generating files", revision, maxRevisions)

	feedback := result.architectReview.Rationale
	if len(result.architectReview.Suggestions) > 0 {
		feedback += "\nSuggestions:\n"
		for _, s := range result.architectReview.Suggestions {
			feedback += "- " + s + "\n"
		}
	}

	// Re-generate each file with architect feedback.
	generatedSiblings := make(map[string]string)
	for _, fc := range plan.FilesToChange {
		if fc.Action == "delete" {
			continue
		}
		fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
		if pathErr != nil {
			continue
		}
		currentContent := ""
		data, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			currentContent = string(data)
		}

		newContent, genCall, genErr := agents.ReviseFileContent(ctx, implClient, implModel, plan, task, fc.Path, currentContent, generatedSiblings, feedback)
		if genErr != nil {
			log.Printf("revision failed for %s: %v", fc.Path, genErr)
			continue
		}
		llmCalls = append(llmCalls, genCall)
		generatedSiblings[fc.Path] = newContent
		os.WriteFile(fullPath, []byte(newContent), 0644)
	}
	for _, fc := range plan.FilesToCreate {
		fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
		if pathErr != nil {
			continue
		}
		currentContent := ""
		data, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			currentContent = string(data)
		}
		newContent, genCall, genErr := agents.ReviseFileContent(ctx, implClient, implModel, plan, task, fc.Path, currentContent, generatedSiblings, feedback)
		if genErr != nil {
			log.Printf("revision failed for %s: %v", fc.Path, genErr)
			continue
		}
		llmCalls = append(llmCalls, genCall)
		generatedSiblings[fc.Path] = newContent
		os.WriteFile(fullPath, []byte(newContent), 0644)
	}
	agentsInvoked = append(agentsInvoked, "implementer-revise")
}

diff := lastResult.diff
newFiles := lastResult.newFiles
verifierReport := lastResult.verifierReport
architectReview := lastResult.architectReview
```

- [ ] **Step 6: Store revision count in run**

After the loop, add:

```go
run.Revisions = revision
```

- [ ] **Step 7: Write test for revision loop**

In `internal/orchestrator/workflow_test.go`, add a test using mock LLM servers:
- First architect call returns `{"recommendation": "revise", "rationale": "fix naming", "suggestions": ["use ErrFoo not ErrBar"], ...}`
- Implementer revision call returns valid file content
- Second architect call returns `{"recommendation": "approve", ...}`
- Assert `result.Run.Revisions == 1`
- Assert final status reflects the second architect review

- [ ] **Step 8: Run tests**

```bash
go test ./internal/... -v
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/orchestrator/workflow.go internal/orchestrator/workflow_test.go internal/agents/implementer.go internal/config/config.go internal/models/run.go configs/experiment.yaml
git commit -m "feat: add architect-to-implementer revision loop

When the architect recommends 'revise', the pipeline now re-invokes the
implementer with the architect's rationale and suggestions, then re-runs
verification and architect review. Limited to max_revisions rounds
(default 1) to prevent infinite loops. Revision count is tracked in
run artifacts."
```

---

## Self-Review Checklist

**1. Spec coverage:** Each of the 5 failures identified in experiment docs has a corresponding task:
- Failure 1 (new-file capture) → Task 1
- Failure 2 (baseline verifier) → Task 2
- Failure 3 (non-Go verifier) → Task 3
- Failure 4 (cross-file consistency) → Task 4
- Failure 5 (revision loop) → Task 5

**2. Placeholder scan:** No TBD, TODO, or "implement later" found. All code blocks contain complete implementations. All test code is concrete with assertions.

**3. Type consistency:**
- `VerifierReport` gains `PatchFailures`, `EnvironmentFailures` in Task 2; used in Task 5's `verifyAndReview` helper. Consistent.
- `GenerateFileContent` gains `siblingContents` param in Task 4; `ReviseFileContent` in Task 5 also takes `siblingContents`. Consistent.
- `ArchitectInput.NewFiles` added in Task 1; used in Task 5's `verifyAndReview` helper. Consistent.
- `Run.NewFiles`, `Run.BaselineCommands`, `Run.Revisions` added across Tasks 1, 2, 5. No conflicts.

**4. Ordering:** Tasks are ordered so each builds on previous work. Task 5 (revision loop) uses the `verifyAndReview` helper which incorporates new-file capture (Task 1) and baseline classification (Task 2). Cross-file consistency (Task 4) improves revision quality since revised files also pass sibling context.
