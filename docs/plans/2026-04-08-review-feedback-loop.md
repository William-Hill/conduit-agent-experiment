# Review Feedback Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `cmd/responder` CLI that fetches PR review comments, classifies actionable ones, dispatches the implementer agent to fix them, and pushes — looping until reviews pass or max iterations hit.

**Architecture:** Three new packages (`internal/responder/comments.go`, `internal/responder/classify.go`, `cmd/responder/main.go`) plus a new `buildPromptFromComments` function in the implementer package. The responder reuses the existing implementer agent, tools, and GitHub adapter. Comments are fetched via `gh` CLI, classified by severity/status, batched by file, and formatted as a prompt for the fix agent.

**Tech Stack:** Go, `gh` CLI, anthropic-sdk-go (existing), BetaToolRunner (existing)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/responder/comments.go` | Fetch PR review comments and approvals via `gh` CLI |
| `internal/responder/comments_test.go` | Unit tests for comment fetching (mock `gh` output) |
| `internal/responder/classify.go` | Filter, normalize severity, group actionable comments by file |
| `internal/responder/classify_test.go` | Unit tests for classification logic |
| `internal/responder/prompt.go` | Format actionable comments into an agent prompt |
| `internal/responder/prompt_test.go` | Unit test for prompt formatting |
| `cmd/responder/main.go` | CLI entrypoint: fetch → classify → fix → push loop |
| `Makefile` | Add `respond` target |

---

### Task 1: Comment Types and Fetcher

**Files:**
- Create: `internal/responder/comments.go`
- Create: `internal/responder/comments_test.go`

- [ ] **Step 1: Write the failing test for comment parsing**

Create `internal/responder/comments_test.go`:

```go
package responder

import (
	"testing"
)

func TestParseInlineComments(t *testing.T) {
	// Simulated gh API JSON output for PR inline comments
	raw := `[
		{
			"user": {"login": "greptile-apps[bot]"},
			"path": "internal/cost/budget.go",
			"line": 27,
			"body": "P1 **PLANNER_MAX_COST is loaded but never enforced**\nNo CheckStep call exists."
		},
		{
			"user": {"login": "coderabbitai[bot]"},
			"path": "internal/archivist/dossier_test.go",
			"line": 38,
			"body": "_⚠️ Potential issue_ | _🟠 Major_\n\n**Avoid potential index panic.**\n\nUse t.Fatalf instead of t.Errorf."
		},
		{
			"user": {"login": "chatgpt-codex-connector[bot]"},
			"path": "cmd/implementer/main.go",
			"line": 141,
			"body": "![P2 Badge](https://img.shields.io/badge/P2-yellow) **Stop CLI flow when budget exceeded**\nRunAgent returns BudgetExceeded but caller ignores it."
		},
		{
			"user": {"login": "coderabbitai[bot]"},
			"path": "docs/JOURNEY.md",
			"line": 144,
			"body": "✅ Addressed in commit 61404af"
		}
	]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatalf("ParseInlineComments: %v", err)
	}
	if len(comments) != 4 {
		t.Fatalf("got %d comments, want 4", len(comments))
	}
	if comments[0].Author != "greptile-apps[bot]" {
		t.Errorf("Author = %q, want greptile-apps[bot]", comments[0].Author)
	}
	if comments[0].File != "internal/cost/budget.go" {
		t.Errorf("File = %q", comments[0].File)
	}
	if comments[0].Line != 27 {
		t.Errorf("Line = %d, want 27", comments[0].Line)
	}
	if comments[3].Status != "addressed" {
		t.Errorf("comment with 'Addressed in commit' should have status=addressed, got %q", comments[3].Status)
	}
}

func TestParseReviews(t *testing.T) {
	raw := `[
		{"author": {"login": "user1"}, "state": "APPROVED"},
		{"author": {"login": "coderabbitai"}, "state": "COMMENTED"}
	]`
	approved, err := HasApproval([]byte(raw))
	if err != nil {
		t.Fatalf("HasApproval: %v", err)
	}
	if !approved {
		t.Error("expected approved=true when APPROVED review exists")
	}
}

func TestParseReviewsNoApproval(t *testing.T) {
	raw := `[
		{"author": {"login": "coderabbitai"}, "state": "COMMENTED"},
		{"author": {"login": "greptile-apps"}, "state": "COMMENTED"}
	]`
	approved, err := HasApproval([]byte(raw))
	if err != nil {
		t.Fatalf("HasApproval: %v", err)
	}
	if approved {
		t.Error("expected approved=false when no APPROVED review")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/responder/... -v -count=1`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Write the implementation**

Create `internal/responder/comments.go`:

```go
package responder

import (
	"encoding/json"
	"strings"
)

// ReviewComment represents a single inline review comment from any tool.
type ReviewComment struct {
	Author string
	File   string
	Line   int
	Body   string
	Status string // "pending", "addressed"
}

// ghInlineComment mirrors the GitHub API JSON shape for PR review comments.
type ghInlineComment struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// ghReview mirrors the GitHub API JSON shape for PR reviews.
type ghReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
}

// ParseInlineComments parses the JSON output of gh api .../pulls/{n}/comments.
func ParseInlineComments(data []byte) ([]ReviewComment, error) {
	var raw []ghInlineComment
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	comments := make([]ReviewComment, 0, len(raw))
	for _, c := range raw {
		status := "pending"
		if isAddressed(c.Body) {
			status = "addressed"
		}
		comments = append(comments, ReviewComment{
			Author: c.User.Login,
			File:   c.Path,
			Line:   c.Line,
			Body:   c.Body,
			Status: status,
		})
	}
	return comments, nil
}

// HasApproval parses the JSON output of gh pr view --json reviews and returns
// true if any review has state "APPROVED".
func HasApproval(data []byte) (bool, error) {
	var reviews []ghReview
	if err := json.Unmarshal(data, &reviews); err != nil {
		return false, err
	}
	for _, r := range reviews {
		if r.State == "APPROVED" {
			return true, nil
		}
	}
	return false, nil
}

// isAddressed detects comments that review tools have marked as resolved.
func isAddressed(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "addressed in commit") ||
		strings.Contains(lower, "✅ addressed")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/responder/... -v -count=1`
Expected: PASS — all 3 tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/responder/comments.go internal/responder/comments_test.go
git commit -m "feat(responder): add comment fetcher with parsing and approval detection"
```

---

### Task 2: Comment Classifier

**Files:**
- Create: `internal/responder/classify.go`
- Create: `internal/responder/classify_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/responder/classify_test.go`:

```go
package responder

import (
	"testing"
)

func TestClassifyFiltersAddressed(t *testing.T) {
	comments := []ReviewComment{
		{Author: "coderabbitai[bot]", File: "main.go", Line: 10, Body: "Fix this bug", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "main.go", Line: 20, Body: "✅ Addressed in commit abc123", Status: "addressed"},
	}
	result := Classify(comments)
	if len(result) != 1 {
		t.Fatalf("got %d actionable, want 1", len(result))
	}
	if result[0].Line != 10 {
		t.Errorf("expected line 10, got %d", result[0].Line)
	}
}

func TestClassifyFiltersNitpicks(t *testing.T) {
	comments := []ReviewComment{
		{Author: "coderabbitai[bot]", File: "main.go", Line: 10, Body: "_⚠️ Potential issue_ | _🟠 Major_\n\nReal bug here.", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "main.go", Line: 20, Body: "🧹 Nitpick comments\n\nConsider renaming.", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 1 {
		t.Fatalf("got %d actionable, want 1", len(result))
	}
	if result[0].Severity != "major" {
		t.Errorf("severity = %q, want major", result[0].Severity)
	}
}

func TestClassifyNormalizesSeverity(t *testing.T) {
	comments := []ReviewComment{
		{Author: "greptile-apps[bot]", File: "a.go", Line: 1, Body: "P1 **Bug here**", Status: "pending"},
		{Author: "greptile-apps[bot]", File: "b.go", Line: 2, Body: "P2 **Minor issue**", Status: "pending"},
		{Author: "chatgpt-codex-connector[bot]", File: "c.go", Line: 3, Body: "![P1 Badge] **Critical**", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "d.go", Line: 4, Body: "_⚠️ Potential issue_ | _🔴 Critical_\n\nBad.", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 4 {
		t.Fatalf("got %d, want 4", len(result))
	}

	expected := []string{"critical", "major", "critical", "critical"}
	for i, r := range result {
		if r.Severity != expected[i] {
			t.Errorf("comment %d: severity = %q, want %q", i, r.Severity, expected[i])
		}
	}
}

func TestClassifySortsBySeverity(t *testing.T) {
	comments := []ReviewComment{
		{Author: "greptile-apps[bot]", File: "a.go", Line: 1, Body: "P2 **Minor**", Status: "pending"},
		{Author: "greptile-apps[bot]", File: "b.go", Line: 2, Body: "P1 **Critical**", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 2 {
		t.Fatalf("got %d, want 2", len(result))
	}
	if result[0].Severity != "critical" {
		t.Errorf("first comment should be critical, got %q", result[0].Severity)
	}
}

func TestClassifyGroupsByFile(t *testing.T) {
	comments := []ReviewComment{
		{Author: "bot", File: "b.go", Line: 10, Body: "P1 fix", Status: "pending"},
		{Author: "bot", File: "a.go", Line: 5, Body: "P1 fix", Status: "pending"},
		{Author: "bot", File: "b.go", Line: 20, Body: "P1 fix2", Status: "pending"},
	}
	result := Classify(comments)
	// Should be grouped: a.go first, then b.go:10, b.go:20
	if result[0].File != "a.go" {
		t.Errorf("expected a.go first (alphabetical grouping), got %q", result[0].File)
	}
	if result[1].File != "b.go" || result[2].File != "b.go" {
		t.Errorf("expected b.go grouped together")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/responder/... -v -count=1`
Expected: FAIL — `Classify` not defined

- [ ] **Step 3: Write the implementation**

Create `internal/responder/classify.go`:

```go
package responder

import (
	"sort"
	"strings"
)

// ActionableComment is a review comment that needs a code fix.
type ActionableComment struct {
	File     string
	Line     int
	Body     string
	Author   string
	Severity string // "critical", "major", "minor"
}

// Classify filters out addressed and nitpick comments, normalizes severity,
// and groups remaining actionable comments by file, sorted by severity.
func Classify(comments []ReviewComment) []ActionableComment {
	var result []ActionableComment

	for _, c := range comments {
		if c.Status == "addressed" {
			continue
		}
		sev := extractSeverity(c.Body, c.Author)
		if sev == "nitpick" || sev == "skip" {
			continue
		}
		result = append(result, ActionableComment{
			File:     c.File,
			Line:     c.Line,
			Body:     c.Body,
			Author:   c.Author,
			Severity: sev,
		})
	}

	// Sort: critical first, then major, then minor. Within same severity, group by file.
	sevOrder := map[string]int{"critical": 0, "major": 1, "minor": 2}
	sort.Slice(result, func(i, j int) bool {
		si, sj := sevOrder[result[i].Severity], sevOrder[result[j].Severity]
		if si != sj {
			return si < sj
		}
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		return result[i].Line < result[j].Line
	})

	return result
}

// extractSeverity normalizes severity across Greptile, CodeRabbit, and Codex.
func extractSeverity(body, author string) string {
	lower := strings.ToLower(body)

	// CodeRabbit nitpick detection
	if strings.Contains(lower, "nitpick") {
		return "nitpick"
	}

	// CodeRabbit walkthrough/summary comments (not actionable)
	if strings.Contains(lower, "walkthrough") || strings.Contains(lower, "📝 walkthrough") {
		return "skip"
	}

	// Greptile severity badges
	if strings.Contains(body, "P1") || strings.Contains(body, "p1.svg") {
		return "critical"
	}
	if strings.Contains(body, "P2") || strings.Contains(body, "p2.svg") {
		return "major"
	}
	if strings.Contains(body, "P3") || strings.Contains(body, "p3.svg") {
		return "nitpick"
	}

	// CodeRabbit severity markers
	if strings.Contains(lower, "🔴 critical") || strings.Contains(lower, "critical_") {
		return "critical"
	}
	if strings.Contains(lower, "🟠 major") {
		return "major"
	}
	if strings.Contains(lower, "🟡 minor") {
		return "minor"
	}

	// Codex P-badges
	if strings.Contains(body, "P1-") {
		return "critical"
	}
	if strings.Contains(body, "P2-") {
		return "major"
	}

	// Default: if it has a warning emoji or "potential issue", treat as major
	if strings.Contains(body, "⚠️") || strings.Contains(lower, "potential issue") {
		return "major"
	}

	return "minor"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/responder/... -v -count=1`
Expected: PASS — all classifier tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/responder/classify.go internal/responder/classify_test.go
git commit -m "feat(responder): add comment classifier with severity normalization"
```

---

### Task 3: Prompt Builder

**Files:**
- Create: `internal/responder/prompt.go`
- Create: `internal/responder/prompt_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/responder/prompt_test.go`:

```go
package responder

import (
	"strings"
	"testing"
)

func TestBuildFixPrompt(t *testing.T) {
	comments := []ActionableComment{
		{File: "internal/cost/budget.go", Line: 27, Body: "PLANNER_MAX_COST loaded but never enforced", Author: "greptile-apps[bot]", Severity: "critical"},
		{File: "internal/cost/budget.go", Line: 75, Body: "Malformed env var silently returns 0", Author: "greptile-apps[bot]", Severity: "major"},
		{File: "cmd/main.go", Line: 141, Body: "BudgetExceeded flag ignored", Author: "codex[bot]", Severity: "major"},
	}

	prompt := BuildFixPrompt(comments)

	if !strings.Contains(prompt, "internal/cost/budget.go") {
		t.Error("prompt should contain file path")
	}
	if !strings.Contains(prompt, "line 27") {
		t.Error("prompt should contain line number")
	}
	if !strings.Contains(prompt, "PLANNER_MAX_COST") {
		t.Error("prompt should contain comment body")
	}
	if !strings.Contains(prompt, "critical") {
		t.Error("prompt should contain severity")
	}
	if !strings.Contains(prompt, "cmd/main.go") {
		t.Error("prompt should contain second file")
	}
}

func TestBuildFixPromptEmpty(t *testing.T) {
	prompt := BuildFixPrompt(nil)
	if prompt != "" {
		t.Errorf("empty comments should produce empty prompt, got %q", prompt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/responder/... -v -count=1`
Expected: FAIL — `BuildFixPrompt` not defined

- [ ] **Step 3: Write the implementation**

Create `internal/responder/prompt.go`:

```go
package responder

import (
	"fmt"
	"strings"
)

const fixSystemPrompt = `You are a code review response agent. You receive review comments from
automated reviewers and must fix each one with minimal, targeted changes.

For each comment:
1. Read the file mentioned in the comment
2. Understand what the reviewer is asking for
3. Make the minimal fix
4. Run "go build ./..." to verify the fix compiles

Do NOT refactor surrounding code. Do NOT add features. Fix exactly what
the reviewer flagged, nothing more. After fixing all comments, run
"go build ./..." one final time and state what you changed.`

// BuildFixPrompt formats actionable comments into a prompt for the fix agent.
func BuildFixPrompt(comments []ActionableComment) string {
	if len(comments) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Review Comments to Address\n\n")
	b.WriteString("Fix each of the following review comments. Make minimal, targeted changes.\n\n")

	currentFile := ""
	for _, c := range comments {
		if c.File != currentFile {
			if currentFile != "" {
				b.WriteString("\n")
			}
			currentFile = c.File
		}
		fmt.Fprintf(&b, "### %s (line %d) [%s, %s]\n\n%s\n\n", c.File, c.Line, c.Author, c.Severity, c.Body)
	}

	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/responder/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/responder/prompt.go internal/responder/prompt_test.go
git commit -m "feat(responder): add prompt builder for fix agent"
```

---

### Task 4: CLI Entrypoint

**Files:**
- Create: `cmd/responder/main.go`
- Modify: `Makefile` — add `respond` target

- [ ] **Step 1: Write the CLI**

Create `cmd/responder/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/implementer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/responder"
)

func main() {
	ctx := context.Background()

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	prNumber := os.Getenv("RESPONDER_PR_NUMBER")
	if prNumber == "" {
		log.Fatal("RESPONDER_PR_NUMBER is required")
	}
	prNum, err := strconv.Atoi(prNumber)
	if err != nil {
		log.Fatalf("invalid RESPONDER_PR_NUMBER %q: %v", prNumber, err)
	}

	owner := envOrDefault("IMPL_REPO_OWNER", "ConduitIO")
	repo := envOrDefault("IMPL_REPO_NAME", "conduit")
	forkOwner := envOrDefault("IMPL_FORK_OWNER", "William-Hill")
	modelName := os.Getenv("RESPONDER_MODEL")
	maxIterations := envIntOrDefault("RESPONDER_MAX_ITERATIONS", 3)
	waitSeconds := envIntOrDefault("RESPONDER_WAIT_SECONDS", 120)
	maxToolIter := 15

	adapter := &github.Adapter{
		Owner:      owner,
		Repo:       repo,
		ForkOwner:  forkOwner,
		BaseBranch: "main",
	}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		log.Printf("=== Responder iteration %d/%d ===", iteration, maxIterations)

		// 1. Fetch review comments
		commentData, err := fetchPRComments(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("fetching comments: %v", err)
		}

		// 2. Check for approval
		reviewData, err := fetchPRReviews(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("fetching reviews: %v", err)
		}
		approved, err := responder.HasApproval(reviewData)
		if err != nil {
			log.Fatalf("parsing reviews: %v", err)
		}
		if approved {
			log.Printf("PR #%d has been approved, exiting", prNum)
			return
		}

		// 3. Parse and classify comments
		comments, err := responder.ParseInlineComments(commentData)
		if err != nil {
			log.Fatalf("parsing comments: %v", err)
		}
		actionable := responder.Classify(comments)
		if len(actionable) == 0 {
			log.Printf("No actionable comments remaining, exiting")
			return
		}
		log.Printf("Found %d actionable comments (of %d total)", len(actionable), len(comments))

		// 4. Get the PR branch name
		branch, err := getPRBranch(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("getting PR branch: %v", err)
		}

		// 5. Clone and checkout the PR branch
		repoDir, err := cloneAndCheckout(ctx, owner, repo, forkOwner, branch)
		if err != nil {
			log.Fatalf("cloning repo: %v", err)
		}
		defer os.RemoveAll(repoDir)

		// 6. Run fix agent
		prompt := responder.BuildFixPrompt(actionable)
		plan := &planner.ImplementationPlan{Markdown: prompt}
		log.Printf("Running fix agent on %d comments...", len(actionable))

		result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxToolIter)
		if err != nil {
			log.Fatalf("fix agent failed: %v", err)
		}
		log.Printf("Fix agent completed in %d iterations", result.Iterations)
		log.Printf("Summary: %s", result.Summary)

		// 7. Check for changes
		diffCmd := exec.CommandContext(ctx, "git", "diff", "--stat")
		diffCmd.Dir = repoDir
		diffOutput, err := diffCmd.Output()
		if err != nil {
			log.Fatalf("git diff failed: %v", err)
		}
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = repoDir
		statusOutput, err := statusCmd.Output()
		if err != nil {
			log.Fatalf("git status failed: %v", err)
		}
		if len(diffOutput) == 0 && len(statusOutput) == 0 {
			log.Printf("No changes produced, skipping push")
			continue
		}
		log.Printf("Changes:\n%s", string(diffOutput))

		// 8. Commit and push
		commitMsg := fmt.Sprintf("fix: address review comments (responder iteration %d)", iteration)
		if err := commitAndPush(ctx, repoDir, branch, forkOwner, owner, commitMsg); err != nil {
			log.Fatalf("commit and push failed: %v", err)
		}
		log.Printf("Pushed iteration %d", iteration)

		// 9. Wait for new reviews
		if iteration < maxIterations {
			log.Printf("Waiting %ds for new reviews...", waitSeconds)
			time.Sleep(time.Duration(waitSeconds) * time.Second)
		}
	}

	log.Printf("Max iterations (%d) reached", maxIterations)
	os.Exit(1)
}

func fetchPRComments(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"api",
		fmt.Sprintf("repos/%s/%s/pulls/%d/comments", adapter.Owner, adapter.Repo, prNum),
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	return cmd.Output()
}

func fetchPRReviews(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "reviews",
		"--jq", ".reviews",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	return cmd.Output()
}

func getPRBranch(ctx context.Context, adapter *github.Adapter, prNum int) (string, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "headRefName",
		"--jq", ".headRefName",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func cloneAndCheckout(ctx context.Context, owner, repo, forkOwner, branch string) (string, error) {
	dir, err := os.MkdirTemp("", "responder-*")
	if err != nil {
		return "", err
	}

	// Clone the fork
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", forkOwner, repo)
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--depth", "50", repoURL, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w\n%s", err, out)
	}

	return dir, nil
}

func commitAndPush(ctx context.Context, repoDir, branch, forkOwner, owner, commitMsg string) error {
	cmds := [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
		{"git", "push", "origin", branch},
	}

	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
```

- [ ] **Step 2: Add Makefile target**

Add to `Makefile`:

```makefile
.PHONY: respond
respond:
	go run ./cmd/responder
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./cmd/responder/`
Expected: exit 0 (may need to resolve the `RunAgent` signature based on whether #21 is merged — if `RunAgent` takes a `maxCost` parameter, pass `0` for no limit)

- [ ] **Step 4: Commit**

```bash
git add cmd/responder/main.go Makefile
git commit -m "feat(responder): add CLI entrypoint with fetch-classify-fix-push loop"
```

---

### Task 5: Integration Test with Real gh Output

**Files:**
- Modify: `internal/responder/comments_test.go` — add test with real PR #22 comment shapes

- [ ] **Step 1: Add integration test for Greptile comment format**

Add to `internal/responder/comments_test.go`:

```go
func TestGreptileCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "greptile-apps[bot]"},
		"path": "internal/cost/budget.go",
		"line": 27,
		"body": "<a href=\"#\"><img alt=\"P1\" src=\"https://greptile-static-assets.s3.amazonaws.com/badges/p1.svg\" align=\"top\"></a> **PLANNER_MAX_COST is loaded but never enforced**\n\nLoadBudget reads PLANNER_MAX_COST but no CheckStep call exists."
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "critical" {
		t.Errorf("Greptile P1 should be critical, got %q", actionable[0].Severity)
	}
}

func TestCodeRabbitCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "coderabbitai[bot]"},
		"path": "internal/archivist/dossier_test.go",
		"line": 38,
		"body": "_⚠️ Potential issue_ | _🟠 Major_\n\n**Avoid potential index panic.**"
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "major" {
		t.Errorf("CodeRabbit Major should be major, got %q", actionable[0].Severity)
	}
}

func TestCodexCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "chatgpt-codex-connector[bot]"},
		"path": "cmd/implementer/main.go",
		"line": 141,
		"body": "**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Stop CLI flow when budget exceeded**"
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "major" {
		t.Errorf("Codex P2 should be major, got %q", actionable[0].Severity)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/responder/... -v -count=1`
Expected: PASS — all tests pass including new integration tests

- [ ] **Step 3: Commit**

```bash
git add internal/responder/comments_test.go
git commit -m "test(responder): add integration tests for real review tool comment formats"
```

---

### Task 6: Full Build Verification and Makefile

- [ ] **Step 1: Run full build**

Run: `go build ./...`
Expected: exit 0

- [ ] **Step 2: Run all tests**

Run: `go test ./internal/responder/... -v -count=1`
Expected: All tests pass

- [ ] **Step 3: Run vet**

Run: `go vet ./internal/responder/... ./cmd/responder/...`
Expected: exit 0

- [ ] **Step 4: Test Makefile target**

Run: `make respond` (will fail because env vars aren't set, but should fail with "ANTHROPIC_API_KEY is required" not a build error)
Expected: `ANTHROPIC_API_KEY is required`

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: review feedback response loop (#18)

Standalone responder CLI that fetches PR review comments from Greptile,
CodeRabbit, and Codex via gh CLI, classifies actionable vs addressed vs
nitpick, dispatches implementer agent to fix, and pushes. Loops up to
N iterations until approved or no comments remain."
```
