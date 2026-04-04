# Milestone 2: Narrow Bug-Fix Pilot — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the pipeline with Implementer, Architect, and Task Selector agents plus GitHub integration to go from issue selection through draft PR creation.

**Architecture:** Two CLI commands (`select` and `run`). `select` scans GitHub issues via `gh` CLI and ranks them with LLM. `run` extends the M1 pipeline with Implementer (two-phase: plan + code gen), Verifier (validates patched worktree), Architect (reviews against ADRs), and GitHub adapter (branch + draft PR). Configurable fork target.

**Tech Stack:** Go 1.24, Cobra CLI, Viper config, openai-go SDK (Gemini endpoint), `gh` CLI for GitHub operations.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/config/config.go` | Add `GitHubConfig` and `MaxFilesChanged` to existing config |
| `internal/models/evaluation.go` | Extend with M2 evaluation struct fields |
| `internal/models/run.go` | Add implementer/architect fields to Run |
| `internal/github/adapter.go` | Wraps `gh` CLI for issues, branches, PRs |
| `internal/github/adapter_test.go` | Tests with mock `gh` output |
| `internal/agents/selector.go` | Task Selector: filters and LLM-ranks GitHub issues |
| `internal/agents/selector_test.go` | Tests for filtering, ranking, JSON parsing |
| `internal/agents/implementer.go` | Two-phase: patch plan + full file generation |
| `internal/agents/implementer_test.go` | Tests for plan parsing, file gen, policy check |
| `internal/agents/architect.go` | Architectural review with supplemental ADR lookup |
| `internal/agents/architect_test.go` | Tests for review parsing, recommendation routing |
| `internal/evaluation/metrics.go` | Build and write Evaluation from WorkflowResult |
| `internal/evaluation/metrics_test.go` | Tests for evaluation building |
| `internal/evaluation/scorecard.go` | Aggregate scorecard from evaluation files |
| `internal/orchestrator/workflow.go` | Add implementer, architect, GitHub stages |
| `internal/orchestrator/policies.go` | Add MaxFilesChanged to Policy |
| `internal/reporting/markdown_report.go` | Add implementer plan, architect review, PR link sections |
| `internal/reporting/json_export.go` | Add WriteEvaluationJSON |
| `cmd/experiment/main.go` | Add `select` and `scorecard` commands |
| `configs/experiment.yaml` | Add `github` section, `max_files_changed` |
| `data/tasks/task-002.json` through `task-006.json` | Pilot issue task files |

---

### Task 1: Config — Add GitHub and MaxFilesChanged

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `configs/experiment.yaml`

- [ ] **Step 1: Write failing test for GitHubConfig loading**

In `internal/config/config_test.go`, add:

```go
func TestLoadGitHubConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "experiment.yaml")
	os.WriteFile(cfgPath, []byte(`
target:
  repo_path: "/tmp/conduit"
  ref: "main"
policy:
  max_difficulty: "L2"
  max_blast_radius: "medium"
  allow_push: true
  allow_merge: false
  require_rationale: true
  max_files_changed: 10
execution:
  use_worktree: true
  timeout_seconds: 300
reporting:
  output_dir: "data/runs"
  formats:
    - json
github:
  owner: "ConduitIO"
  repo: "conduit"
  fork_owner: "William-Hill"
  base_branch: "main"
`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.GitHub.Owner != "ConduitIO" {
		t.Errorf("GitHub.Owner = %q, want ConduitIO", cfg.GitHub.Owner)
	}
	if cfg.GitHub.Repo != "conduit" {
		t.Errorf("GitHub.Repo = %q, want conduit", cfg.GitHub.Repo)
	}
	if cfg.GitHub.ForkOwner != "William-Hill" {
		t.Errorf("GitHub.ForkOwner = %q, want William-Hill", cfg.GitHub.ForkOwner)
	}
	if cfg.GitHub.BaseBranch != "main" {
		t.Errorf("GitHub.BaseBranch = %q, want main", cfg.GitHub.BaseBranch)
	}
	if cfg.Policy.MaxFilesChanged != 10 {
		t.Errorf("Policy.MaxFilesChanged = %d, want 10", cfg.Policy.MaxFilesChanged)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/config/ -run TestLoadGitHubConfig -v`
Expected: FAIL — `cfg.GitHub` undefined

- [ ] **Step 3: Add GitHubConfig and MaxFilesChanged to config structs**

In `internal/config/config.go`, add the `GitHubConfig` struct and add it to `Config`. Add `MaxFilesChanged` to `PolicyConfig`:

```go
type GitHubConfig struct {
	Owner      string `mapstructure:"owner"`
	Repo       string `mapstructure:"repo"`
	ForkOwner  string `mapstructure:"fork_owner"`
	BaseBranch string `mapstructure:"base_branch"`
}
```

Update `Config` to include `GitHub GitHubConfig \`mapstructure:"github"\``.

Update `PolicyConfig` to include `MaxFilesChanged int \`mapstructure:"max_files_changed"\``.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/config/ -run TestLoadGitHubConfig -v`
Expected: PASS

- [ ] **Step 5: Update experiment.yaml**

Add to `configs/experiment.yaml`:

```yaml
github:
  owner: "ConduitIO"
  repo: "conduit"
  fork_owner: "William-Hill"
  base_branch: "main"
```

Add `max_files_changed: 10` under `policy`.

- [ ] **Step 6: Run all config tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/config/ -v`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go configs/experiment.yaml
git commit -m "feat: add GitHub config and max_files_changed policy"
```

---

### Task 2: Models — Extend Run and Evaluation

**Files:**
- Modify: `internal/models/run.go`
- Modify: `internal/models/evaluation.go`

- [ ] **Step 1: Add Implementer and Architect fields to Run**

In `internal/models/run.go`, add these fields to the `Run` struct after the existing `VerifierSummary` field:

```go
	ImplementerPlan    string `json:"implementer_plan,omitempty"`
	ImplementerDiff    string `json:"implementer_diff,omitempty"`
	ArchitectDecision  string `json:"architect_decision,omitempty"`
	ArchitectReview    string `json:"architect_review,omitempty"`
	PRURL              string `json:"pr_url,omitempty"`
```

- [ ] **Step 2: Extend Evaluation struct for M2**

Replace the `Evaluation` struct in `internal/models/evaluation.go` with:

```go
// Evaluation captures the assessment of a completed run.
type Evaluation struct {
	RunID               string      `json:"run_id"`
	TaskID              string      `json:"task_id"`
	IssueNumber         int         `json:"issue_number,omitempty"`
	Difficulty          string      `json:"difficulty"`
	BlastRadius         string      `json:"blast_radius"`
	TriageDecision      string      `json:"triage_decision"`
	ImplementerSuccess  bool        `json:"implementer_success"`
	FilesChanged        int         `json:"files_changed"`
	DiffLines           int         `json:"diff_lines"`
	VerifierPass        bool        `json:"verifier_pass"`
	ArchitectDecision   string      `json:"architect_decision"`
	ArchitectConfidence string      `json:"architect_confidence"`
	PRCreated           bool        `json:"pr_created"`
	PRURL               string      `json:"pr_url,omitempty"`
	FailureMode         FailureMode `json:"failure_mode,omitempty"`
	FailureDetail       string      `json:"failure_detail,omitempty"`
	TotalDurationMs     int64       `json:"total_duration_ms"`
	LLMCalls            int         `json:"llm_calls"`
	LLMTokensUsed       int         `json:"llm_tokens_used,omitempty"`
	LintPass            bool        `json:"lint_pass"`
	BuildPass           bool        `json:"build_pass"`
	TestsPass           bool        `json:"tests_pass"`
	ReviewScore         int         `json:"review_score"`
	ArchitectureScore   int         `json:"architecture_score"`
	Notes               string      `json:"notes,omitempty"`
}
```

- [ ] **Step 3: Add IssueNumber field to Task**

In `internal/models/task.go`, add to the `Task` struct:

```go
	IssueNumber int `json:"issue_number,omitempty"`
```

- [ ] **Step 4: Verify build compiles**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go build ./...`
Expected: Success (no compilation errors)

- [ ] **Step 5: Commit**

```bash
git add internal/models/run.go internal/models/evaluation.go internal/models/task.go
git commit -m "feat: extend Run and Evaluation models for milestone 2"
```

---

### Task 3: Policy — Add MaxFilesChanged Check

**Files:**
- Modify: `internal/orchestrator/policies.go`
- Modify: `internal/orchestrator/policies_test.go` (or create if not existing)

- [ ] **Step 1: Write failing test for MaxFilesChanged**

Create or update `internal/orchestrator/policies_test.go`:

```go
package orchestrator

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCheckTaskPass(t *testing.T) {
	policy := DefaultPhase1Policy()
	task := models.Task{Difficulty: models.DifficultyL1, BlastRadius: models.BlastRadiusLow}
	if err := policy.CheckTask(task); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestCheckTaskDifficultyExceeded(t *testing.T) {
	policy := DefaultPhase1Policy()
	task := models.Task{Difficulty: models.DifficultyL3, BlastRadius: models.BlastRadiusLow}
	if err := policy.CheckTask(task); err == nil {
		t.Error("expected error for L3 task")
	}
}

func TestCheckPatchBreadthPass(t *testing.T) {
	policy := DefaultPhase1Policy()
	if err := policy.CheckPatchBreadth(5); err != nil {
		t.Errorf("expected pass for 5 files, got: %v", err)
	}
}

func TestCheckPatchBreadthExceeded(t *testing.T) {
	policy := DefaultPhase1Policy()
	if err := policy.CheckPatchBreadth(15); err == nil {
		t.Error("expected error for 15 files when max is 10")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/orchestrator/ -run TestCheckPatchBreadth -v`
Expected: FAIL — `CheckPatchBreadth` undefined

- [ ] **Step 3: Add MaxFilesChanged to Policy and CheckPatchBreadth method**

In `internal/orchestrator/policies.go`, add `MaxFilesChanged` field to `Policy`:

```go
type Policy struct {
	MaxDifficulty    models.Difficulty  `json:"max_difficulty"`
	MaxBlastRadius   models.BlastRadius `json:"max_blast_radius"`
	AllowPush        bool               `json:"allow_push"`
	AllowMerge       bool               `json:"allow_merge"`
	RequireRationale bool               `json:"require_rationale"`
	MaxFilesChanged  int                `json:"max_files_changed"`
}
```

Update `DefaultPhase1Policy()` to set `MaxFilesChanged: 10`.

Add the method:

```go
// CheckPatchBreadth returns an error if the number of files exceeds the policy limit.
func (p Policy) CheckPatchBreadth(numFiles int) error {
	if p.MaxFilesChanged > 0 && numFiles > p.MaxFilesChanged {
		return fmt.Errorf("patch touches %d files, exceeds policy max %d", numFiles, p.MaxFilesChanged)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/orchestrator/ -run TestCheckPatchBreadth -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/policies.go internal/orchestrator/policies_test.go
git commit -m "feat: add MaxFilesChanged policy check"
```

---

### Task 4: GitHub Adapter

**Files:**
- Create: `internal/github/adapter.go`
- Create: `internal/github/adapter_test.go`

- [ ] **Step 1: Write failing test for ListIssues**

Create `internal/github/adapter_test.go`:

```go
package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	os.WriteFile(path, []byte(content), 0755)
	return path
}

func TestListIssues(t *testing.T) {
	dir := t.TempDir()
	// Create a mock gh script that outputs JSON
	script := writeScript(t, dir, "gh", `#!/bin/sh
echo '[{"number":123,"title":"test issue","labels":[{"name":"bug"}],"body":"fix this","createdAt":"2026-01-01T00:00:00Z","comments":[],"assignees":[]}]'
`)

	adapter := &Adapter{
		Owner:      "ConduitIO",
		Repo:       "conduit",
		BaseBranch: "main",
		ForkOwner:  "William-Hill",
		GHPath:     script,
	}

	issues, err := adapter.ListIssues(context.Background(), IssueListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues() error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].Number != 123 {
		t.Errorf("issue number = %d, want 123", issues[0].Number)
	}
	if issues[0].Title != "test issue" {
		t.Errorf("issue title = %q, want 'test issue'", issues[0].Title)
	}
}

func TestListIssuesWithLabels(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "gh", `#!/bin/sh
# Verify --label flag is passed
if echo "$@" | grep -q "label"; then
  echo '[{"number":1,"title":"labeled","labels":[],"body":"","createdAt":"2026-01-01T00:00:00Z","comments":[],"assignees":[]}]'
else
  echo '[]'
fi
`)

	adapter := &Adapter{
		Owner:  "ConduitIO",
		Repo:   "conduit",
		GHPath: script,
	}

	issues, err := adapter.ListIssues(context.Background(), IssueListOpts{Limit: 10, Labels: []string{"bug"}})
	if err != nil {
		t.Fatalf("ListIssues() error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/github/ -run TestListIssues -v`
Expected: FAIL — package/types undefined

- [ ] **Step 3: Implement Adapter with ListIssues and GetIssue**

Create `internal/github/adapter.go`:

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Issue represents a GitHub issue fetched via gh CLI.
type Issue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Labels    []Label  `json:"labels"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"createdAt"`
	Comments  []any    `json:"comments"`
	Assignees []any    `json:"assignees"`
}

// Label represents a GitHub issue label.
type Label struct {
	Name string `json:"name"`
}

// IssueListOpts configures issue listing.
type IssueListOpts struct {
	Limit  int
	Labels []string
}

// DraftPRInput holds the data for creating a draft PR.
type DraftPRInput struct {
	Title    string
	Body     string
	Head     string // branch name (on fork)
	Base     string // target branch on upstream
}

// Adapter wraps the gh CLI for GitHub operations.
type Adapter struct {
	Owner      string
	Repo       string
	BaseBranch string
	ForkOwner  string
	GHPath     string // path to gh binary, defaults to "gh"
}

func (a *Adapter) ghPath() string {
	if a.GHPath != "" {
		return a.GHPath
	}
	return "gh"
}

func (a *Adapter) repoSlug() string {
	return a.Owner + "/" + a.Repo
}

func (a *Adapter) forkSlug() string {
	if a.ForkOwner != "" {
		return a.ForkOwner + "/" + a.Repo
	}
	return a.repoSlug()
}

// ListIssues fetches open issues from the repository.
func (a *Adapter) ListIssues(ctx context.Context, opts IssueListOpts) ([]Issue, error) {
	args := []string{"issue", "list",
		"--repo", a.repoSlug(),
		"--state", "open",
		"--limit", fmt.Sprintf("%d", opts.Limit),
		"--json", "number,title,labels,body,createdAt,comments,assignees",
	}
	if len(opts.Labels) > 0 {
		args = append(args, "--label", strings.Join(opts.Labels, ","))
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}

	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parsing issues JSON: %w", err)
	}
	return issues, nil
}

// GetIssue fetches a single issue by number.
func (a *Adapter) GetIssue(ctx context.Context, number int) (*Issue, error) {
	args := []string{"issue", "view",
		fmt.Sprintf("%d", number),
		"--repo", a.repoSlug(),
		"--json", "number,title,labels,body,createdAt,comments,assignees",
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("getting issue %d: %w", number, err)
	}

	var issue Issue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, fmt.Errorf("parsing issue JSON: %w", err)
	}
	return &issue, nil
}

// CreateBranchAndPush creates a branch in the worktree, commits changes, and pushes to the fork.
func (a *Adapter) CreateBranchAndPush(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	cmds := [][]string{
		{"git", "checkout", "-b", branch},
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
		{"git", "push", "-u", "origin", branch},
	}

	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = worktreeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("running %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// CreateDraftPR opens a draft pull request and returns the PR URL.
func (a *Adapter) CreateDraftPR(ctx context.Context, pr DraftPRInput) (string, error) {
	head := fmt.Sprintf("%s:%s", a.ForkOwner, pr.Head)
	args := []string{"pr", "create",
		"--repo", a.repoSlug(),
		"--title", pr.Title,
		"--body", pr.Body,
		"--head", head,
		"--base", pr.Base,
		"--draft",
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("creating draft PR: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *Adapter) runGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, a.ghPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/github/ -v`
Expected: PASS

- [ ] **Step 5: Write test for CreateDraftPR**

Add to `internal/github/adapter_test.go`:

```go
func TestCreateDraftPR(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "gh", `#!/bin/sh
echo "https://github.com/ConduitIO/conduit/pull/999"
`)

	adapter := &Adapter{
		Owner:      "ConduitIO",
		Repo:       "conduit",
		BaseBranch: "main",
		ForkOwner:  "William-Hill",
		GHPath:     script,
	}

	url, err := adapter.CreateDraftPR(context.Background(), DraftPRInput{
		Title: "Fix: test",
		Body:  "## Task\ntest",
		Head:  "agent/task-123-test",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreateDraftPR() error: %v", err)
	}
	if url != "https://github.com/ConduitIO/conduit/pull/999" {
		t.Errorf("url = %q, want PR URL", url)
	}
}
```

- [ ] **Step 6: Run all GitHub adapter tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/github/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat: implement GitHub adapter wrapping gh CLI"
```

---

### Task 5: Implementer Agent — Phase 1 (Patch Plan)

**Files:**
- Create: `internal/agents/implementer.go`
- Create: `internal/agents/implementer_test.go`

- [ ] **Step 1: Write failing test for PatchPlan parsing**

Create `internal/agents/implementer_test.go`:

```go
package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCreatePatchPlan(t *testing.T) {
	llmResponse := `{
		"plan_summary": "Catch version-parse errors per YAML document and continue",
		"files_to_change": [
			{"path": "pkg/provisioning/service.go", "action": "modify", "description": "Wrap version parse in per-doc error handler"}
		],
		"files_to_create": [],
		"design_choices": ["Isolate errors per document"],
		"assumptions": ["No callers depend on fail-fast"],
		"test_recommendations": ["Add multi-doc YAML test with invalid version"]
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{
		ID:          "task-002",
		Title:       "Fix YAML provisioning failure",
		Description: "Multi-pipeline YAML fails if one has invalid version",
	}
	dossier := models.Dossier{
		TaskID:       "task-002",
		Summary:      "Provisioning loop fails on bad version",
		RelatedFiles: []string{"pkg/provisioning/service.go"},
	}

	plan, llmCall, err := CreatePatchPlan(context.Background(), client, "gemini-2.5-flash", task, dossier, nil)
	if err != nil {
		t.Fatalf("CreatePatchPlan() error: %v", err)
	}
	if plan.PlanSummary == "" {
		t.Error("plan summary is empty")
	}
	if len(plan.FilesToChange) != 1 {
		t.Errorf("files to change = %d, want 1", len(plan.FilesToChange))
	}
	if plan.FilesToChange[0].Path != "pkg/provisioning/service.go" {
		t.Errorf("file path = %q, want pkg/provisioning/service.go", plan.FilesToChange[0].Path)
	}
	if llmCall.Agent != "implementer" {
		t.Errorf("agent = %q, want implementer", llmCall.Agent)
	}
}

func TestCreatePatchPlanBadJSON(t *testing.T) {
	server := mockLLMServer(t, "not valid json")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")
	task := models.Task{ID: "task-002", Title: "test"}
	dossier := models.Dossier{TaskID: "task-002"}

	_, _, err := CreatePatchPlan(context.Background(), client, "gemini-2.5-flash", task, dossier, nil)
	if err == nil {
		t.Error("expected error on bad JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run TestCreatePatchPlan -v`
Expected: FAIL — `CreatePatchPlan` undefined

- [ ] **Step 3: Implement CreatePatchPlan**

Create `internal/agents/implementer.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const implementerPlanSystemPrompt = `You are an expert software engineer planning a narrow patch for an open source project. Given a maintenance task, its context dossier, and relevant file contents, propose the smallest possible change that satisfies the task.

Respond with a JSON object containing exactly these fields:
- "plan_summary": one paragraph describing the approach
- "files_to_change": array of objects with "path", "action" (modify/delete), and "description"
- "files_to_create": array of objects with "path" and "description"
- "design_choices": array of key design decisions
- "assumptions": array of assumptions being made
- "test_recommendations": array of recommended test changes

Respond ONLY with the JSON object, no markdown fences or extra text.`

const implementerCodeSystemPrompt = `You are an expert software engineer implementing a narrow patch. Given a file's current contents and a patch plan, produce the complete updated file contents.

Return ONLY the complete file contents, no markdown fences, no explanations, no diff markers. The output must be valid, compilable code that can be written directly to the file.`

// PatchPlan describes the Implementer's planned changes.
type PatchPlan struct {
	PlanSummary         string       `json:"plan_summary"`
	FilesToChange       []FileChange `json:"files_to_change"`
	FilesToCreate       []FileCreate `json:"files_to_create"`
	DesignChoices       []string     `json:"design_choices"`
	Assumptions         []string     `json:"assumptions"`
	TestRecommendations []string     `json:"test_recommendations"`
}

// FileChange describes a modification to an existing file.
type FileChange struct {
	Path        string `json:"path"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

// FileCreate describes a new file to create.
type FileCreate struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

// TotalFiles returns the total number of files in the plan.
func (p PatchPlan) TotalFiles() int {
	return len(p.FilesToChange) + len(p.FilesToCreate)
}

// CreatePatchPlan asks the LLM to produce a patch plan for the task.
// fileContents maps file paths to their contents for context.
func CreatePatchPlan(ctx context.Context, client *llm.Client, modelName string, task models.Task, dossier models.Dossier, fileContents map[string]string) (PatchPlan, models.LLMCall, error) {
	userPrompt := buildPlanPrompt(task, dossier, fileContents)

	start := time.Now()
	response, err := client.Complete(ctx, implementerPlanSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "implementer",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return PatchPlan{}, call, fmt.Errorf("implementer plan LLM call failed: %w", err)
	}

	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var plan PatchPlan
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return PatchPlan{}, call, fmt.Errorf("parsing patch plan JSON: %w", err)
	}

	return plan, call, nil
}

// GenerateFileContent asks the LLM to produce updated file contents.
// Returns the new file content as a string.
func GenerateFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string) (string, models.LLMCall, error) {
	userPrompt := buildCodeGenPrompt(plan, task, filePath, currentContent)

	start := time.Now()
	response, err := client.Complete(ctx, implementerCodeSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "implementer",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return "", call, fmt.Errorf("implementer code gen failed for %s: %w", filePath, err)
	}

	// Strip markdown fences if the LLM wrapped them
	content := strings.TrimSpace(response)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			lines = lines[1 : len(lines)-1]
			content = strings.Join(lines, "\n")
		}
	}

	return content, call, nil
}

// ReadFileContents reads the contents of files from the worktree, up to maxSize bytes.
// Files larger than maxSize are truncated with a marker.
func ReadFileContents(worktreeDir string, paths []string, maxSize int64) map[string]string {
	contents := make(map[string]string)
	for _, p := range paths {
		fullPath := worktreeDir + "/" + p
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		if info.Size() > maxSize {
			data = append(data[:maxSize], []byte("\n\n// ... FILE TRUNCATED (too large for context) ...\n")...)
		}
		contents[p] = string(data)
	}
	return contents
}

func buildPlanPrompt(task models.Task, dossier models.Dossier, fileContents map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n")
	fmt.Fprintf(&b, "Title: %s\n", task.Title)
	fmt.Fprintf(&b, "Description: %s\n", task.Description)
	if len(task.AcceptanceCriteria) > 0 {
		fmt.Fprintf(&b, "\nAcceptance Criteria:\n")
		for _, c := range task.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}

	fmt.Fprintf(&b, "\n## Dossier\n")
	fmt.Fprintf(&b, "Summary: %s\n", dossier.Summary)
	if len(dossier.Risks) > 0 {
		fmt.Fprintf(&b, "Risks: %s\n", strings.Join(dossier.Risks, "; "))
	}
	if len(dossier.OpenQuestions) > 0 {
		fmt.Fprintf(&b, "Open Questions: %s\n", strings.Join(dossier.OpenQuestions, "; "))
	}

	fmt.Fprintf(&b, "\n## Relevant Files\n")
	for _, f := range dossier.RelatedFiles {
		fmt.Fprintf(&b, "- %s\n", f)
	}

	if len(fileContents) > 0 {
		fmt.Fprintf(&b, "\n## File Contents\n")
		for path, content := range fileContents {
			fmt.Fprintf(&b, "\n### %s\n```\n%s\n```\n", path, content)
		}
	}

	return b.String()
}

func buildCodeGenPrompt(plan PatchPlan, task models.Task, filePath, currentContent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Patch Plan\n%s\n\n", plan.PlanSummary)

	for _, fc := range plan.FilesToChange {
		if fc.Path == filePath {
			fmt.Fprintf(&b, "## Change for this file\n%s\n\n", fc.Description)
			break
		}
	}

	fmt.Fprintf(&b, "## Task Context\n%s\n\n", task.Description)
	fmt.Fprintf(&b, "## Current File: %s\n```\n%s\n```\n", filePath, currentContent)
	fmt.Fprintf(&b, "\nProduce the complete updated file contents.")

	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run TestCreatePatchPlan -v`
Expected: PASS

- [ ] **Step 5: Write test for GenerateFileContent**

Add to `internal/agents/implementer_test.go`:

```go
func TestGenerateFileContent(t *testing.T) {
	llmResponse := `package main

func hello() string {
	return "fixed"
}
`
	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	plan := PatchPlan{PlanSummary: "Fix the function"}
	task := models.Task{ID: "task-002", Description: "Fix the bug"}

	content, call, err := GenerateFileContent(context.Background(), client, "gemini-2.5-flash", plan, task, "main.go", "package main\n\nfunc hello() string { return \"broken\" }\n")
	if err != nil {
		t.Fatalf("GenerateFileContent() error: %v", err)
	}
	if !strings.Contains(content, "fixed") {
		t.Errorf("expected content to contain 'fixed', got: %s", content)
	}
	if call.Agent != "implementer" {
		t.Errorf("agent = %q, want implementer", call.Agent)
	}
}

func TestReadFileContents(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b"), 0644)

	contents := ReadFileContents(dir, []string{"a.go", "b.go", "missing.go"}, 32*1024)
	if len(contents) != 2 {
		t.Errorf("got %d files, want 2", len(contents))
	}
	if contents["a.go"] != "package a" {
		t.Errorf("a.go = %q", contents["a.go"])
	}
}
```

Add these imports to the test file: `"os"`, `"path/filepath"`, `"strings"`.

- [ ] **Step 6: Run all implementer tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run "TestCreatePatchPlan|TestGenerateFileContent|TestReadFileContents" -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agents/implementer.go internal/agents/implementer_test.go
git commit -m "feat: implement Implementer agent with patch plan and code generation"
```

---

### Task 6: Architect Agent

**Files:**
- Create: `internal/agents/architect.go`
- Create: `internal/agents/architect_test.go`

- [ ] **Step 1: Write failing test for ArchitectReview**

Create `internal/agents/architect_test.go`:

```go
package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestArchitectReviewApprove(t *testing.T) {
	llmResponse := `{
		"recommendation": "approve",
		"confidence": "high",
		"alignment_notes": "Change is contained to provisioning loop",
		"risks_identified": [],
		"adr_conflicts": [],
		"suggestions": ["Add a log line"],
		"rationale": "Narrow fix, consistent with existing patterns"
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	review, call, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", ArchitectInput{
		Diff:    "--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new",
		Dossier: models.Dossier{TaskID: "task-002", Summary: "fix provisioning"},
		Plan:    PatchPlan{PlanSummary: "catch errors per doc"},
		VerifierReport: VerifierReport{OverallPass: true, Summary: "2/2 passed"},
		SupplementalDocs: map[string]string{"docs/adr/001.md": "# ADR 001"},
	})
	if err != nil {
		t.Fatalf("ArchitectReview() error: %v", err)
	}
	if review.Recommendation != "approve" {
		t.Errorf("recommendation = %q, want approve", review.Recommendation)
	}
	if review.Confidence != "high" {
		t.Errorf("confidence = %q, want high", review.Confidence)
	}
	if call.Agent != "architect" {
		t.Errorf("agent = %q, want architect", call.Agent)
	}
}

func TestArchitectReviewReject(t *testing.T) {
	llmResponse := `{
		"recommendation": "reject",
		"confidence": "high",
		"alignment_notes": "Change modifies runtime semantics",
		"risks_identified": ["breaks backwards compatibility"],
		"adr_conflicts": ["ADR-003 forbids this pattern"],
		"suggestions": [],
		"rationale": "Too broad for this task level"
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	review, _, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", ArchitectInput{
		Diff:    "big diff",
		Dossier: models.Dossier{TaskID: "task-002"},
		Plan:    PatchPlan{PlanSummary: "risky change"},
		VerifierReport: VerifierReport{OverallPass: false},
	})
	if err != nil {
		t.Fatalf("ArchitectReview() error: %v", err)
	}
	if review.Recommendation != "reject" {
		t.Errorf("recommendation = %q, want reject", review.Recommendation)
	}
}

func TestArchitectReviewBadJSON(t *testing.T) {
	server := mockLLMServer(t, "not json")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	_, _, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", ArchitectInput{
		Diff:    "diff",
		Dossier: models.Dossier{},
		Plan:    PatchPlan{},
		VerifierReport: VerifierReport{},
	})
	if err == nil {
		t.Error("expected error on bad JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run TestArchitectReview -v`
Expected: FAIL — `ArchitectReview` undefined

- [ ] **Step 3: Implement ArchitectReview**

Create `internal/agents/architect.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const architectSystemPrompt = `You are a senior software architect reviewing a proposed patch for an open source project. Evaluate the patch for architectural alignment, semantic safety, and reviewability.

Consider:
1. Does the patch stay within the subsystem's boundaries?
2. Does it contradict any ADR guidance?
3. Are there semantic risks (behavior changes, compatibility breaks, concurrency concerns)?
4. Is the diff minimal and reviewable?
5. Does the verification report support confidence in the change?
6. Are the implementer's stated assumptions valid?

Respond with a JSON object containing exactly these fields:
- "recommendation": one of "approve", "revise", "reject"
- "confidence": one of "high", "medium", "low"
- "alignment_notes": brief description of architectural alignment
- "risks_identified": array of identified risks
- "adr_conflicts": array of ADR conflicts found
- "suggestions": array of improvement suggestions
- "rationale": one paragraph explaining the recommendation

Respond ONLY with the JSON object, no markdown fences or extra text.`

// ArchitectInput holds everything the Architect needs to review.
type ArchitectInput struct {
	Diff             string
	Dossier          models.Dossier
	Plan             PatchPlan
	VerifierReport   VerifierReport
	SupplementalDocs map[string]string // path -> content of ADRs/docs for touched files
}

// ArchitectReviewResult holds the Architect's assessment.
type ArchitectReviewResult struct {
	Recommendation string   `json:"recommendation"` // approve, revise, reject
	Confidence     string   `json:"confidence"`      // high, medium, low
	AlignmentNotes string   `json:"alignment_notes"`
	RisksIdentified []string `json:"risks_identified"`
	ADRConflicts   []string `json:"adr_conflicts"`
	Suggestions    []string `json:"suggestions"`
	Rationale      string   `json:"rationale"`
}

// ArchitectReview sends the patch and context to the LLM for architectural review.
func ArchitectReview(ctx context.Context, client *llm.Client, modelName string, input ArchitectInput) (ArchitectReviewResult, models.LLMCall, error) {
	userPrompt := buildArchitectPrompt(input)

	start := time.Now()
	response, err := client.Complete(ctx, architectSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "architect",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return ArchitectReviewResult{}, call, fmt.Errorf("architect LLM call failed: %w", err)
	}

	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var result ArchitectReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return ArchitectReviewResult{}, call, fmt.Errorf("parsing architect review JSON: %w", err)
	}

	return result, call, nil
}

func buildArchitectPrompt(input ArchitectInput) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Patch Plan\n%s\n\n", input.Plan.PlanSummary)
	fmt.Fprintf(&b, "Design choices: %s\n", strings.Join(input.Plan.DesignChoices, "; "))
	fmt.Fprintf(&b, "Assumptions: %s\n\n", strings.Join(input.Plan.Assumptions, "; "))

	fmt.Fprintf(&b, "## Dossier Summary\n%s\n\n", input.Dossier.Summary)
	if len(input.Dossier.Risks) > 0 {
		fmt.Fprintf(&b, "Known risks: %s\n\n", strings.Join(input.Dossier.Risks, "; "))
	}

	fmt.Fprintf(&b, "## Verification\nOverall pass: %v\n%s\n\n", input.VerifierReport.OverallPass, input.VerifierReport.Summary)

	fmt.Fprintf(&b, "## Diff\n```\n%s\n```\n\n", input.Diff)

	if len(input.SupplementalDocs) > 0 {
		fmt.Fprintf(&b, "## Related Architecture Docs\n")
		for path, content := range input.SupplementalDocs {
			fmt.Fprintf(&b, "\n### %s\n%s\n", path, content)
		}
	}

	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run TestArchitectReview -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agents/architect.go internal/agents/architect_test.go
git commit -m "feat: implement Architect agent for patch review"
```

---

### Task 7: Task Selector Agent

**Files:**
- Create: `internal/agents/selector.go`
- Create: `internal/agents/selector_test.go`

- [ ] **Step 1: Write failing test for FilterIssues**

Create `internal/agents/selector_test.go`:

```go
package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
)

func TestFilterIssues(t *testing.T) {
	issues := []github.Issue{
		{Number: 1, Title: "Simple bug", Body: "short description", Labels: []github.Label{{Name: "bug"}}},
		{Number: 2, Title: "Epic rewrite", Body: "redesign the entire system with breaking changes", Labels: []github.Label{{Name: "epic"}}},
		{Number: 3, Title: "Assigned issue", Body: "already taken", Assignees: []any{"someone"}},
		{Number: 4, Title: "Docs fix", Body: "update readme", Labels: []github.Label{{Name: "docs"}}},
	}

	filtered := FilterIssues(issues)
	if len(filtered) != 2 {
		t.Errorf("got %d issues, want 2 (should exclude epic and assigned)", len(filtered))
	}
	for _, f := range filtered {
		if f.Number == 2 {
			t.Error("should have filtered out epic issue")
		}
		if f.Number == 3 {
			t.Error("should have filtered out assigned issue")
		}
	}
}

func TestRankIssues(t *testing.T) {
	llmResponse := `[
		{"number": 1, "difficulty": "L1", "blast_radius": "low", "rationale": "Simple bug fix", "acceptance_criteria": ["Fix the bug"]},
		{"number": 4, "difficulty": "L1", "blast_radius": "low", "rationale": "Docs update", "acceptance_criteria": ["Update docs"]}
	]`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	issues := []github.Issue{
		{Number: 1, Title: "Simple bug", Body: "short"},
		{Number: 4, Title: "Docs fix", Body: "update"},
	}

	ranked, _, err := RankIssues(context.Background(), client, "gemini-2.5-flash", issues)
	if err != nil {
		t.Fatalf("RankIssues() error: %v", err)
	}
	if len(ranked) != 2 {
		t.Errorf("got %d ranked, want 2", len(ranked))
	}
	if ranked[0].Number != 1 {
		t.Errorf("first ranked number = %d, want 1", ranked[0].Number)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run "TestFilterIssues|TestRankIssues" -v`
Expected: FAIL — `FilterIssues` undefined

- [ ] **Step 3: Implement FilterIssues and RankIssues**

Create `internal/agents/selector.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const selectorSystemPrompt = `You are a maintenance task selector for an open source project. Given a list of GitHub issues, rank them by suitability for automated narrow fixes.

Criteria:
- Is this a bug fix, docs issue, config mismatch, dependency bump, or narrow improvement?
- Can it be resolved with changes to 5 or fewer files?
- Are reproduction steps or acceptance criteria clear?
- Estimated difficulty: L1 (docs/deps/lint), L2 (narrow bug fix, config alignment), L3 (contained features), L4 (runtime semantics)
- Estimated blast radius: low, medium, high

Return a JSON array of objects, one per issue, ranked from most to least suitable:
[{"number": 123, "difficulty": "L1", "blast_radius": "low", "rationale": "...", "acceptance_criteria": ["..."]}]

Only include issues that are L1 or L2 difficulty. Exclude L3+ issues entirely.
Respond ONLY with the JSON array, no markdown fences or extra text.`

// excludedLabels are labels that indicate an issue should not be selected.
var excludedLabels = map[string]bool{
	"epic": true, "arch-v2": true, "wontfix": true, "duplicate": true,
}

// excludedKeywords in issue body indicate the issue is too broad.
var excludedKeywords = []string{"redesign", "breaking change", "rewrite", "refactor entire"}

// RankedIssue is an LLM-ranked GitHub issue with metadata.
type RankedIssue struct {
	Number             int      `json:"number"`
	Difficulty         string   `json:"difficulty"`
	BlastRadius        string   `json:"blast_radius"`
	Rationale          string   `json:"rationale"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// FilterIssues applies heuristic pre-filters to exclude issues that are clearly out of scope.
func FilterIssues(issues []github.Issue) []github.Issue {
	var filtered []github.Issue
	for _, issue := range issues {
		if len(issue.Assignees) > 0 {
			continue
		}

		excluded := false
		for _, label := range issue.Labels {
			if excludedLabels[label.Name] {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		body := strings.ToLower(issue.Body)
		for _, kw := range excludedKeywords {
			if strings.Contains(body, kw) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		if len(issue.Body) > 2000 {
			continue
		}

		filtered = append(filtered, issue)
	}
	return filtered
}

// RankIssues uses the LLM to rank filtered issues by suitability.
func RankIssues(ctx context.Context, client *llm.Client, modelName string, issues []github.Issue) ([]RankedIssue, models.LLMCall, error) {
	userPrompt := buildSelectorPrompt(issues)

	start := time.Now()
	response, err := client.Complete(ctx, selectorSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "selector",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return nil, call, fmt.Errorf("selector LLM call failed: %w", err)
	}

	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var ranked []RankedIssue
	if err := json.Unmarshal([]byte(cleaned), &ranked); err != nil {
		return nil, call, fmt.Errorf("parsing ranked issues JSON: %w", err)
	}

	return ranked, call, nil
}

// RankedToTask converts a RankedIssue and its GitHub Issue into a Task.
func RankedToTask(ranked RankedIssue, issue github.Issue) models.Task {
	return models.Task{
		ID:                 fmt.Sprintf("task-gh-%d", ranked.Number),
		Title:              issue.Title,
		Source:             fmt.Sprintf("github#%d", ranked.Number),
		Description:        issue.Body,
		Difficulty:         models.Difficulty(ranked.Difficulty),
		BlastRadius:        models.BlastRadius(ranked.BlastRadius),
		AcceptanceCriteria: ranked.AcceptanceCriteria,
		IssueNumber:        ranked.Number,
		Status:             models.TaskStatusPending,
	}
}

func buildSelectorPrompt(issues []github.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## GitHub Issues (%d total)\n\n", len(issues))
	for _, issue := range issues {
		fmt.Fprintf(&b, "### #%d: %s\n", issue.Number, issue.Title)
		var labelNames []string
		for _, l := range issue.Labels {
			labelNames = append(labelNames, l.Name)
		}
		if len(labelNames) > 0 {
			fmt.Fprintf(&b, "Labels: %s\n", strings.Join(labelNames, ", "))
		}
		fmt.Fprintf(&b, "%s\n\n", issue.Body)
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run "TestFilterIssues|TestRankIssues" -v`
Expected: PASS

- [ ] **Step 5: Write test for RankedToTask**

Add to `internal/agents/selector_test.go`:

```go
func TestRankedToTask(t *testing.T) {
	ranked := RankedIssue{
		Number:             123,
		Difficulty:         "L1",
		BlastRadius:        "low",
		AcceptanceCriteria: []string{"Fix it"},
	}
	issue := github.Issue{
		Number: 123,
		Title:  "Bug in parsing",
		Body:   "Parsing fails on edge case",
	}

	task := RankedToTask(ranked, issue)
	if task.ID != "task-gh-123" {
		t.Errorf("task ID = %q, want task-gh-123", task.ID)
	}
	if task.IssueNumber != 123 {
		t.Errorf("issue number = %d, want 123", task.IssueNumber)
	}
	if task.Difficulty != models.DifficultyL1 {
		t.Errorf("difficulty = %q, want L1", task.Difficulty)
	}
}
```

Add `"github.com/mjhilldigital/conduit-agent-experiment/internal/models"` to the imports.

- [ ] **Step 6: Run all selector tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/agents/ -run "TestFilterIssues|TestRankIssues|TestRankedToTask" -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agents/selector.go internal/agents/selector_test.go
git commit -m "feat: implement Task Selector agent with filtering and LLM ranking"
```

---

### Task 8: Evaluation Metrics

**Files:**
- Create: `internal/evaluation/metrics.go`
- Create: `internal/evaluation/metrics_test.go`

- [ ] **Step 1: Write failing test for BuildEvaluation**

Create `internal/evaluation/metrics_test.go`:

```go
package evaluation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestBuildEvaluation(t *testing.T) {
	eval := BuildEvaluation(EvalInput{
		RunID:               "run-001",
		TaskID:              "task-002",
		IssueNumber:         2255,
		Difficulty:          "L1",
		BlastRadius:         "low",
		TriageDecision:      "accept",
		ImplementerSuccess:  true,
		FilesChanged:        1,
		DiffLines:           15,
		VerifierPass:        true,
		ArchitectDecision:   "approve",
		ArchitectConfidence: "high",
		PRCreated:           true,
		PRURL:               "https://github.com/ConduitIO/conduit/pull/999",
		TotalDurationMs:     5000,
		LLMCalls:            4,
	})

	if eval.RunID != "run-001" {
		t.Errorf("RunID = %q", eval.RunID)
	}
	if eval.PRCreated != true {
		t.Error("PRCreated should be true")
	}
	if eval.FailureMode != "" {
		t.Errorf("FailureMode should be empty, got %q", eval.FailureMode)
	}
}

func TestWriteAndLoadEvaluation(t *testing.T) {
	dir := t.TempDir()
	eval := models.Evaluation{
		RunID:  "run-001",
		TaskID: "task-002",
	}

	if err := WriteEvaluationJSON(dir, eval); err != nil {
		t.Fatalf("WriteEvaluationJSON() error: %v", err)
	}

	path := filepath.Join(dir, "evaluation.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if len(data) == 0 {
		t.Error("file is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/evaluation/ -v`
Expected: FAIL — `BuildEvaluation` undefined

- [ ] **Step 3: Implement BuildEvaluation and WriteEvaluationJSON**

Create `internal/evaluation/metrics.go`:

```go
package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// EvalInput holds the data needed to build an Evaluation.
type EvalInput struct {
	RunID               string
	TaskID              string
	IssueNumber         int
	Difficulty          string
	BlastRadius         string
	TriageDecision      string
	ImplementerSuccess  bool
	FilesChanged        int
	DiffLines           int
	VerifierPass        bool
	ArchitectDecision   string
	ArchitectConfidence string
	PRCreated           bool
	PRURL               string
	FailureMode         models.FailureMode
	FailureDetail       string
	TotalDurationMs     int64
	LLMCalls            int
	LLMTokensUsed       int
}

// BuildEvaluation constructs an Evaluation from pipeline results.
func BuildEvaluation(input EvalInput) models.Evaluation {
	return models.Evaluation{
		RunID:               input.RunID,
		TaskID:              input.TaskID,
		IssueNumber:         input.IssueNumber,
		Difficulty:          input.Difficulty,
		BlastRadius:         input.BlastRadius,
		TriageDecision:      input.TriageDecision,
		ImplementerSuccess:  input.ImplementerSuccess,
		FilesChanged:        input.FilesChanged,
		DiffLines:           input.DiffLines,
		VerifierPass:        input.VerifierPass,
		ArchitectDecision:   input.ArchitectDecision,
		ArchitectConfidence: input.ArchitectConfidence,
		PRCreated:           input.PRCreated,
		PRURL:               input.PRURL,
		FailureMode:         input.FailureMode,
		FailureDetail:       input.FailureDetail,
		TotalDurationMs:     input.TotalDurationMs,
		LLMCalls:            input.LLMCalls,
		LLMTokensUsed:       input.LLMTokensUsed,
	}
}

// WriteEvaluationJSON writes the evaluation as JSON to the given directory.
func WriteEvaluationJSON(dir string, eval models.Evaluation) error {
	data, err := json.MarshalIndent(eval, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling evaluation: %w", err)
	}
	path := filepath.Join(dir, "evaluation.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/evaluation/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evaluation/metrics.go internal/evaluation/metrics_test.go
git commit -m "feat: implement evaluation metrics builder"
```

---

### Task 9: Scorecard Aggregation

**Files:**
- Create: `internal/evaluation/scorecard.go`
- Create: `internal/evaluation/scorecard_test.go`

- [ ] **Step 1: Write failing test for GenerateScorecard**

Create `internal/evaluation/scorecard_test.go`:

```go
package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestGenerateScorecard(t *testing.T) {
	dir := t.TempDir()

	// Create two evaluation files in subdirs
	for _, sub := range []string{"run-001", "run-002"} {
		subDir := filepath.Join(dir, sub)
		os.MkdirAll(subDir, 0755)
		eval := models.Evaluation{
			RunID:              sub,
			TaskID:             "task-" + sub,
			Difficulty:         "L1",
			TriageDecision:     "accept",
			ImplementerSuccess: sub == "run-001", // one success, one failure
			VerifierPass:       sub == "run-001",
			ArchitectDecision:  "approve",
			PRCreated:          sub == "run-001",
			FilesChanged:       2,
			DiffLines:          10,
			LLMCalls:           3,
		}
		if sub == "run-002" {
			eval.FailureMode = models.FailureHallucination
			eval.ArchitectDecision = "reject"
		}
		data, _ := json.MarshalIndent(eval, "", "  ")
		os.WriteFile(filepath.Join(subDir, "evaluation.json"), data, 0644)
	}

	sc, err := GenerateScorecard(dir)
	if err != nil {
		t.Fatalf("GenerateScorecard() error: %v", err)
	}
	if sc.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", sc.TotalRuns)
	}
	if sc.PRsCreated != 1 {
		t.Errorf("PRsCreated = %d, want 1", sc.PRsCreated)
	}
	if sc.FailureModes[string(models.FailureHallucination)] != 1 {
		t.Errorf("expected 1 hallucination failure")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/evaluation/ -run TestGenerateScorecard -v`
Expected: FAIL — `GenerateScorecard` undefined

- [ ] **Step 3: Implement GenerateScorecard**

Create `internal/evaluation/scorecard.go`:

```go
package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Scorecard aggregates evaluation results across all runs.
type Scorecard struct {
	TotalRuns          int            `json:"total_runs"`
	SuccessfulRuns     int            `json:"successful_runs"`
	PRsCreated         int            `json:"prs_created"`
	AvgFilesChanged    float64        `json:"avg_files_changed"`
	AvgDiffLines       float64        `json:"avg_diff_lines"`
	AvgLLMCalls        float64        `json:"avg_llm_calls"`
	SuccessByDifficulty map[string]int `json:"success_by_difficulty"`
	FailureModes       map[string]int `json:"failure_modes"`
}

// GenerateScorecard reads all evaluation.json files under runsDir and aggregates them.
func GenerateScorecard(runsDir string) (Scorecard, error) {
	var evals []models.Evaluation

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return Scorecard{}, fmt.Errorf("reading runs dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		evalPath := filepath.Join(runsDir, entry.Name(), "evaluation.json")
		data, err := os.ReadFile(evalPath)
		if err != nil {
			continue // skip runs without evaluation
		}
		var eval models.Evaluation
		if err := json.Unmarshal(data, &eval); err != nil {
			continue
		}
		evals = append(evals, eval)
	}

	if len(evals) == 0 {
		return Scorecard{}, fmt.Errorf("no evaluation files found in %s", runsDir)
	}

	sc := Scorecard{
		TotalRuns:           len(evals),
		SuccessByDifficulty: make(map[string]int),
		FailureModes:        make(map[string]int),
	}

	var totalFiles, totalDiff, totalLLM int
	for _, e := range evals {
		if e.PRCreated {
			sc.PRsCreated++
		}
		if e.ImplementerSuccess && e.VerifierPass {
			sc.SuccessfulRuns++
			sc.SuccessByDifficulty[e.Difficulty]++
		}
		if e.FailureMode != "" {
			sc.FailureModes[string(e.FailureMode)]++
		}
		totalFiles += e.FilesChanged
		totalDiff += e.DiffLines
		totalLLM += e.LLMCalls
	}

	n := float64(len(evals))
	sc.AvgFilesChanged = float64(totalFiles) / n
	sc.AvgDiffLines = float64(totalDiff) / n
	sc.AvgLLMCalls = float64(totalLLM) / n

	return sc, nil
}

// FormatScorecard returns a human-readable summary of the scorecard.
func FormatScorecard(sc Scorecard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Experiment Scorecard\n\n")
	fmt.Fprintf(&b, "| Metric | Value |\n|--------|-------|\n")
	fmt.Fprintf(&b, "| Total runs | %d |\n", sc.TotalRuns)
	fmt.Fprintf(&b, "| Successful | %d (%.0f%%) |\n", sc.SuccessfulRuns, float64(sc.SuccessfulRuns)/float64(sc.TotalRuns)*100)
	fmt.Fprintf(&b, "| PRs created | %d |\n", sc.PRsCreated)
	fmt.Fprintf(&b, "| Avg files changed | %.1f |\n", sc.AvgFilesChanged)
	fmt.Fprintf(&b, "| Avg diff lines | %.1f |\n", sc.AvgDiffLines)
	fmt.Fprintf(&b, "| Avg LLM calls | %.1f |\n", sc.AvgLLMCalls)

	if len(sc.SuccessByDifficulty) > 0 {
		fmt.Fprintf(&b, "\n### Success by Difficulty\n\n")
		for diff, count := range sc.SuccessByDifficulty {
			fmt.Fprintf(&b, "- %s: %d\n", diff, count)
		}
	}

	if len(sc.FailureModes) > 0 {
		fmt.Fprintf(&b, "\n### Failure Modes\n\n")
		for mode, count := range sc.FailureModes {
			fmt.Fprintf(&b, "- %s: %d\n", mode, count)
		}
	}

	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/evaluation/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evaluation/scorecard.go internal/evaluation/scorecard_test.go
git commit -m "feat: implement scorecard aggregation"
```

---

### Task 10: Reporting — Add M2 Sections

**Files:**
- Modify: `internal/reporting/markdown_report.go`
- Modify: `internal/reporting/markdown_report_test.go`
- Modify: `internal/reporting/json_export.go`

- [ ] **Step 1: Write failing test for M2 report sections**

Add to `internal/reporting/markdown_report_test.go` a test that passes a Run with M2 fields:

```go
func TestRenderMarkdownWithM2Fields(t *testing.T) {
	run := models.Run{
		ID:                "run-002",
		TaskID:            "task-002",
		StartedAt:         time.Now(),
		EndedAt:           time.Now(),
		AgentsInvoked:     []string{"triage", "archivist", "implementer", "verifier", "architect"},
		FinalStatus:       models.RunStatusSuccess,
		HumanDecision:     models.HumanDecisionPending,
		TriageDecision:    "accept",
		TriageReason:      "within policy",
		ImplementerPlan:   "Fix the provisioning loop error handling",
		ImplementerDiff:   "--- a/file.go\n+++ b/file.go",
		ArchitectDecision: "approve",
		ArchitectReview:   "Change is well-contained",
		PRURL:             "https://github.com/ConduitIO/conduit/pull/999",
	}
	dossier := models.Dossier{
		TaskID:  "task-002",
		Summary: "Fix provisioning",
	}
	task := models.Task{
		ID:    "task-002",
		Title: "Fix YAML provisioning",
	}

	md, err := RenderMarkdown(run, dossier, task)
	if err != nil {
		t.Fatalf("RenderMarkdown() error: %v", err)
	}
	if !strings.Contains(md, "Patch Plan") {
		t.Error("missing Patch Plan section")
	}
	if !strings.Contains(md, "Architect Review") {
		t.Error("missing Architect Review section")
	}
	if !strings.Contains(md, "pull/999") {
		t.Error("missing PR URL")
	}
}
```

Add `"strings"` and `"time"` to imports if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/reporting/ -run TestRenderMarkdownWithM2Fields -v`
Expected: FAIL — template doesn't include M2 fields

- [ ] **Step 3: Update the markdown template**

In `internal/reporting/markdown_report.go`, update `reportTemplate` to add these sections after the Verification section and before Run Details:

```
{{ if .Run.ImplementerPlan }}
## Patch Plan

{{ .Run.ImplementerPlan }}
{{ end }}
{{ if .Run.ImplementerDiff }}
## Diff

` + "```" + `
{{ .Run.ImplementerDiff }}
` + "```" + `
{{ end }}
{{ if .Run.ArchitectDecision }}
## Architect Review

| Field | Value |
|-------|-------|
| Decision | {{ .Run.ArchitectDecision }} |

{{ .Run.ArchitectReview }}
{{ end }}
{{ if .Run.PRURL }}
## Pull Request

{{ .Run.PRURL }}
{{ end }}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/reporting/ -v`
Expected: PASS

- [ ] **Step 5: Verify build compiles**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go build ./...`
Expected: Success

- [ ] **Step 7: Commit**

```bash
git add internal/reporting/markdown_report.go internal/reporting/markdown_report_test.go
git commit -m "feat: add M2 report sections to markdown report"
```

---

### Task 11: Orchestrator — Integrate M2 Pipeline Stages

**Files:**
- Modify: `internal/orchestrator/workflow.go`
- Modify: `internal/orchestrator/workflow_test.go`

- [ ] **Step 1: Update WorkflowResult for M2**

In `internal/orchestrator/workflow.go`, update the `WorkflowResult` struct:

```go
type WorkflowResult struct {
	Run            models.Run
	Dossier        models.Dossier
	Task           models.Task
	TriageDecision agents.TriageDecision
	PatchPlan      agents.PatchPlan
	VerifierReport agents.VerifierReport
	ArchitectReview agents.ArchitectReviewResult
	Evaluation     models.Evaluation
	LLMCalls       []models.LLMCall
	PRURL          string
}
```

- [ ] **Step 2: Extend RunWorkflow with M2 stages**

Rewrite `RunWorkflow` in `internal/orchestrator/workflow.go` to add the Implementer, Architect, and GitHub stages between the existing Archivist/Triage and Verifier stages. The updated function should:

1. Keep existing: Triage -> repo walk -> Archivist -> Triage re-check
2. Add: Setup worktree early (before Implementer Phase 1)
3. Add: Implementer Phase 1 (patch plan) with policy.CheckPatchBreadth
4. Add: Implementer Phase 2 (generate files, write to worktree)
5. Keep: Verifier (now validates patched worktree)
6. Add: Generate diff via `git diff` in worktree
7. Add: Architect review
8. Add: If approved and config allows push, create branch + push + draft PR via GitHub adapter
9. Add: Build evaluation
10. Cleanup worktree

The function signature changes to accept the GitHub adapter:

```go
func RunWorkflow(ctx context.Context, task models.Task, cfg config.Config, mcfg config.ModelsConfig, ghAdapter *github.Adapter) (*WorkflowResult, error)
```

Pass `nil` for `ghAdapter` when GitHub operations should be skipped (e.g., in tests or when `allow_push` is false).

Key implementation details:
- Read file contents for Implementer Phase 1 using `agents.ReadFileContents(runner.WorkDir, dossier.RelatedFiles[:min(10, len(dossier.RelatedFiles))], 32*1024)`
- After Implementer Phase 2 writes files, run `git diff` in worktree to get the diff
- For supplemental ADR docs for the Architect, search for files matching `**/adr/**` or `**/design-doc*/**` that reference packages of changed files
- Build evaluation using `evaluation.BuildEvaluation()` before returning
- Track all LLM calls from all agents in the `llmCalls` slice

- [ ] **Step 3: Update existing test and write M2 integration test**

The existing `TestRunWorkflow` in `workflow_test.go` will break because `RunWorkflow` now takes a `*github.Adapter` parameter. Update the existing test call to pass `nil` for the adapter.

Then add a new test for the M2 pipeline. The test should:
- Create a mock LLM server that returns different responses based on the system prompt (archivist, implementer plan, implementer code, architect)
- Create a temp git repo with a sample file
- Run `RunWorkflow` with `ghAdapter: nil` (skip PR creation)
- Verify the result has a non-empty PatchPlan, ArchitectReview, and Evaluation

```go
func TestRunWorkflowM2(t *testing.T) {
	// Create a temp dir with git init and a sample file
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.MkdirAll(filepath.Join(repoDir, "docs", "adr"), 0755)
	os.WriteFile(filepath.Join(repoDir, "docs", "adr", "001.md"), []byte("# ADR 001\nDo things right.\n"), 0644)
	runGit(t, repoDir, "add", "-A")
	runGit(t, repoDir, "commit", "-m", "init")

	server := newMultiResponseLLMServer(t)
	defer server.Close()

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Policy:    config.PolicyConfig{MaxDifficulty: "L2", MaxBlastRadius: "medium", MaxFilesChanged: 10},
		Execution: config.ExecutionConfig{UseWorktree: true, TimeoutSeconds: 30},
		Reporting: config.ReportingConfig{OutputDir: t.TempDir()},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		APIKey:   "test-key",
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}, "implementer": {Model: "test"}, "architect": {Model: "test"}},
	}
	task := models.Task{
		ID:          "task-002",
		Title:       "Fix the bug",
		Description: "Fix main.go",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.PatchPlan.PlanSummary == "" {
		t.Error("PatchPlan.PlanSummary is empty")
	}
	if result.ArchitectReview.Recommendation == "" {
		t.Error("ArchitectReview.Recommendation is empty")
	}
	if len(result.LLMCalls) < 3 {
		t.Errorf("expected at least 3 LLM calls, got %d", len(result.LLMCalls))
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newMultiResponseLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		systemPrompt := ""
		if len(req.Messages) > 0 {
			systemPrompt = req.Messages[0].Content
		}

		var content string
		switch {
		case strings.Contains(systemPrompt, "archivist"):
			content = `{"summary":"fix main.go","relevant_files":["main.go"],"relevant_docs":["docs/adr/001.md"],"suggested_commands":["go build ./..."],"risks":[],"open_questions":[]}`
		case strings.Contains(systemPrompt, "planning a narrow patch"):
			content = `{"plan_summary":"Fix the main function","files_to_change":[{"path":"main.go","action":"modify","description":"Fix the function"}],"files_to_create":[],"design_choices":["minimal change"],"assumptions":["safe"],"test_recommendations":["test it"]}`
		case strings.Contains(systemPrompt, "implementing a narrow patch"):
			content = "package main\n\nfunc main() {\n\t// fixed\n}\n"
		case strings.Contains(systemPrompt, "architect"):
			content = `{"recommendation":"approve","confidence":"high","alignment_notes":"good","risks_identified":[],"adr_conflicts":[],"suggestions":[],"rationale":"looks good"}`
		default:
			content = `{"summary":"default"}`
		}

		resp := map[string]any{
			"id": "test", "object": "chat.completion", "created": 1, "model": "test",
			"choices": []map[string]any{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": content},
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 50, "total_tokens": 60},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}
```

Add necessary imports: `"context"`, `"encoding/json"`, `"net/http"`, `"net/http/httptest"`, `"os"`, `"os/exec"`, `"path/filepath"`, `"strings"`, `"testing"` and the project packages.

- [ ] **Step 4: Run the integration test**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./internal/orchestrator/ -run TestRunWorkflowM2 -v -timeout 60s`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./... -timeout 120s`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/workflow.go internal/orchestrator/workflow_test.go
git commit -m "feat: integrate M2 pipeline stages into orchestrator"
```

---

### Task 12: CLI — Add Select and Scorecard Commands

**Files:**
- Modify: `cmd/experiment/main.go`

- [ ] **Step 1: Add the `select` command**

Add to `cmd/experiment/main.go` a `newSelectCmd()` function:

```go
func newSelectCmd() *cobra.Command {
	var limit int
	var labels []string
	var modelsFile string

	cmd := &cobra.Command{
		Use:   "select",
		Short: "Scan GitHub issues and produce ranked task JSONs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			mcfg, err := config.LoadModels(modelsFile)
			if err != nil {
				return fmt.Errorf("loading models config: %w", err)
			}
			if mcfg.APIKey == "" {
				return fmt.Errorf("GEMINI_API_KEY env var is required")
			}

			ghAdapter := &github.Adapter{
				Owner:      cfg.GitHub.Owner,
				Repo:       cfg.GitHub.Repo,
				BaseBranch: cfg.GitHub.BaseBranch,
				ForkOwner:  cfg.GitHub.ForkOwner,
			}

			fmt.Printf("Fetching issues from %s/%s...\n", cfg.GitHub.Owner, cfg.GitHub.Repo)
			issues, err := ghAdapter.ListIssues(cmd.Context(), github.IssueListOpts{Limit: 100, Labels: labels})
			if err != nil {
				return fmt.Errorf("listing issues: %w", err)
			}
			fmt.Printf("Fetched %d issues\n", len(issues))

			filtered := agents.FilterIssues(issues)
			fmt.Printf("After filtering: %d issues\n", len(filtered))

			selectorModel := "gemini-2.5-flash"
			if rc, ok := mcfg.Roles["selector"]; ok {
				selectorModel = rc.Model
			}
			llmClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, selectorModel)

			ranked, _, err := agents.RankIssues(cmd.Context(), llmClient, selectorModel, filtered)
			if err != nil {
				return fmt.Errorf("ranking issues: %w", err)
			}

			if limit > 0 && len(ranked) > limit {
				ranked = ranked[:limit]
			}

			fmt.Printf("\n%-8s %-6s %-8s %s\n", "Issue", "Level", "Blast", "Rationale")
			fmt.Println(strings.Repeat("-", 70))
			for _, r := range ranked {
				fmt.Printf("#%-7d %-6s %-8s %s\n", r.Number, r.Difficulty, r.BlastRadius, r.Rationale)
			}

			// Build a map of issue number -> Issue for task creation
			issueMap := make(map[int]github.Issue)
			for _, issue := range issues {
				issueMap[issue.Number] = issue
			}

			for _, r := range ranked {
				issue, ok := issueMap[r.Number]
				if !ok {
					continue
				}
				task := agents.RankedToTask(r, issue)
				data, _ := json.MarshalIndent(task, "", "  ")
				taskPath := filepath.Join("data", "tasks", fmt.Sprintf("task-gh-%d.json", r.Number))
				if err := os.WriteFile(taskPath, data, 0644); err != nil {
					return fmt.Errorf("writing task file: %w", err)
				}
				fmt.Printf("Wrote %s\n", taskPath)
			}

			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "max number of tasks to select")
	cmd.Flags().StringSliceVar(&labels, "labels", nil, "filter issues by labels")
	cmd.Flags().StringVar(&modelsFile, "models", "configs/models.yaml", "models config file path")
	return cmd
}
```

Add the imports: `"strings"`, `"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"`, `"github.com/mjhilldigital/conduit-agent-experiment/internal/github"`, `"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"`.

- [ ] **Step 2: Add the `scorecard` command**

Add `newScorecardCmd()`:

```go
func newScorecardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scorecard",
		Short: "Display aggregate scorecard from all evaluation files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			sc, err := evaluation.GenerateScorecard(cfg.Reporting.OutputDir)
			if err != nil {
				return fmt.Errorf("generating scorecard: %w", err)
			}

			fmt.Print(evaluation.FormatScorecard(sc))
			return nil
		},
	}
}
```

Add the import: `"github.com/mjhilldigital/conduit-agent-experiment/internal/evaluation"`.

- [ ] **Step 3: Register new commands and update run command**

In `main()`, register the new commands:

```go
root.AddCommand(newSelectCmd())
root.AddCommand(newScorecardCmd())
```

Update `newRunCmd()` to:
1. Create the `github.Adapter` from config
2. Pass it to `orchestrator.RunWorkflow()`
3. Write `evaluation.json` alongside other artifacts
4. Print PR URL if created

- [ ] **Step 4: Verify build compiles**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go build ./cmd/experiment/`
Expected: Success

- [ ] **Step 5: Run all tests**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./... -timeout 120s`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add cmd/experiment/main.go
git commit -m "feat: add select and scorecard CLI commands"
```

---

### Task 13: Seed Pilot Task Files

**Files:**
- Create: `data/tasks/task-gh-2255.json`
- Create: `data/tasks/task-gh-576.json`
- Create: `data/tasks/task-gh-645.json`
- Create: `data/tasks/task-gh-2061.json`
- Create: `data/tasks/task-gh-1999.json`

- [ ] **Step 1: Create task-gh-2255.json**

```json
{
  "id": "task-gh-2255",
  "title": "Fix multi-pipeline YAML provisioning failure on invalid version",
  "source": "github#2255",
  "description": "When a multi-document YAML file contains one pipeline section with an unrecognized version (e.g., version: 4), the entire provisioning run fails with a hard error instead of skipping the bad document and continuing with the valid ones. The fix should catch version-parse errors per document and continue rather than aborting the batch.",
  "labels": ["bug"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "Valid pipelines in a multi-doc YAML are provisioned even if one doc has an invalid version",
    "Invalid version documents produce a warning log, not a fatal error",
    "Existing single-doc YAML provisioning behavior is unchanged"
  ],
  "issue_number": 2255,
  "status": "pending"
}
```

- [ ] **Step 2: Create task-gh-576.json**

```json
{
  "id": "task-gh-576",
  "title": "Return proper HTTP status codes instead of 500 for validation errors",
  "source": "github#576",
  "description": "All API errors are returned as HTTP 500 regardless of cause. Starting a pipeline with no connectors returns 500 instead of 400, and creating a connector with invalid config returns 500 instead of 400. Error messages are also not documented in the Swagger/OpenAPI spec.",
  "labels": ["bug"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "Validation errors return HTTP 400 instead of 500",
    "At least the two documented examples return correct status codes",
    "No changes to successful request behavior"
  ],
  "issue_number": 576,
  "status": "pending"
}
```

- [ ] **Step 3: Create task-gh-645.json**

```json
{
  "id": "task-gh-645",
  "title": "Automate version constant update in built-in connectors",
  "source": "github#645",
  "description": "Built-in connectors each have a manually maintained version constant that diverges from actual release tags. The fix is a CI action that ensures the version constant matches the release tag at release time.",
  "labels": ["housekeeping"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "GitHub Actions workflow that checks or updates version constants at tag time",
    "Version constant format is documented",
    "Existing connector behavior is unchanged"
  ],
  "issue_number": 645,
  "status": "pending"
}
```

- [ ] **Step 4: Create task-gh-2061.json**

```json
{
  "id": "task-gh-2061",
  "title": "Fix pipeline status stopped in config file still running on restart",
  "source": "github#2061",
  "description": "A pipeline declared with status: stopped in a YAML config file still starts running on conduit run when it was previously running. The provisioner reads the stored runtime state instead of the file-declared status on restart.",
  "labels": ["bug"],
  "difficulty": "L2",
  "blast_radius": "medium",
  "acceptance_criteria": [
    "Pipeline with status: stopped in config does not start on restart",
    "Pipeline with status: running in config continues to start normally",
    "State reconciliation prefers file-declared status over stored runtime state"
  ],
  "issue_number": 2061,
  "status": "pending"
}
```

- [ ] **Step 5: Create task-gh-1999.json**

```json
{
  "id": "task-gh-1999",
  "title": "Fix LifecycleOnCreated backwards compatibility for old connectors",
  "source": "github#1999",
  "description": "Old connectors that don't implement LifecycleOnCreated fail hard instead of gracefully falling back. The gRPC call returns 'unknown method' and kills the pipeline instead of being a no-op. The fix is to catch the unknown method error and treat it as a no-op.",
  "labels": ["bug"],
  "difficulty": "L2",
  "blast_radius": "medium",
  "acceptance_criteria": [
    "Connectors without LifecycleOnCreated do not crash the pipeline",
    "Unknown method errors from lifecycle calls are treated as no-ops",
    "Connectors that do implement lifecycle methods still work correctly"
  ],
  "issue_number": 1999,
  "status": "pending"
}
```

- [ ] **Step 6: Commit**

```bash
git add data/tasks/task-gh-2255.json data/tasks/task-gh-576.json data/tasks/task-gh-645.json data/tasks/task-gh-2061.json data/tasks/task-gh-1999.json
git commit -m "feat: seed 5 pilot task files from Conduit GitHub issues"
```

---

### Task 14: Update models.yaml for Selector Role

**Files:**
- Modify: `configs/models.yaml`

- [ ] **Step 1: Add selector role**

Add to `configs/models.yaml` under `roles`:

```yaml
  selector:
    model: "gemini-2.5-flash"
```

- [ ] **Step 2: Verify build still works**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go build ./cmd/experiment/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add configs/models.yaml
git commit -m "feat: add selector role to models config"
```

---

### Task 15: End-to-End Smoke Test

**Files:** None created — this is a validation task.

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go test ./... -timeout 120s -v`
Expected: All tests pass

- [ ] **Step 2: Verify CLI builds and shows help**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go run ./cmd/experiment/ --help`
Expected: Shows `index`, `run`, `select`, `scorecard`, `report` commands

- [ ] **Step 3: Verify select command help**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go run ./cmd/experiment/ select --help`
Expected: Shows `--limit`, `--labels`, `--models` flags

- [ ] **Step 4: Verify scorecard command help**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go run ./cmd/experiment/ scorecard --help`
Expected: Shows scorecard description

- [ ] **Step 5: Run vet and lint**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go vet ./...`
Expected: No issues

- [ ] **Step 6: Commit any fixes if needed**

Only if steps 1-5 revealed issues that need fixing.
