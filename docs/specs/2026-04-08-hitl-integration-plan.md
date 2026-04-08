# HITL Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add human-in-the-loop approval gates to the agent pipeline using GitHub labels, comments, and `gh` CLI polling, with three operating modes (full/yolo/custom).

**Architecture:** New `internal/hitl/` package provides gate logic (label polling, PR state polling, comment management). The orchestrator (`cmd/implementer/main.go`) calls into hitl gates at two points: after triage (Gate 1) and after PR creation (Gate 3). The responder (`cmd/responder/main.go`) gains conversation resolution and bot review re-triggering capabilities.

**Tech Stack:** Go, `gh` CLI, GitHub API (via `gh api`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/hitl/config.go` | HITL configuration (mode, gate flags, intervals, bot reviewers) |
| `internal/hitl/config_test.go` | Config loading tests |
| `internal/hitl/labels.go` | Label operations (apply, remove, check) via `gh` CLI |
| `internal/hitl/labels_test.go` | Label operation tests with mock `gh` |
| `internal/hitl/gates.go` | Gate logic: WaitForLabel, WaitForPRAction polling loops |
| `internal/hitl/gates_test.go` | Gate polling tests |
| `internal/hitl/comments.go` | Comment posting, bot review triggering, thread resolution |
| `internal/hitl/comments_test.go` | Comment/thread tests with mock `gh` |
| `cmd/implementer/main.go` | Modified: add Gate 1 after triage, Gate 3 after PR creation |
| `cmd/responder/main.go` | Modified: add thread resolution, bot review re-triggering |
| `internal/github/adapter.go` | Modified: add AddLabel, RemoveLabel, GetLabels, PostComment methods |

---

### Task 1: HITL Config

**Files:**
- Create: `internal/hitl/config.go`
- Create: `internal/hitl/config_test.go`

- [ ] **Step 1: Write the failing test for config loading**

```go
// internal/hitl/config_test.go
package hitl

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg := LoadConfig()

	if cfg.Mode != "full" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "full")
	}
	if !cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be true by default")
	}
	if !cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be true by default")
	}
	if cfg.Gate1PollInterval != 5*time.Minute {
		t.Errorf("Gate1PollInterval = %v, want %v", cfg.Gate1PollInterval, 5*time.Minute)
	}
	if cfg.Gate3PollInterval != 5*time.Minute {
		t.Errorf("Gate3PollInterval = %v, want %v", cfg.Gate3PollInterval, 5*time.Minute)
	}
	if !cfg.ResolveBotComments {
		t.Error("ResolveBotComments should be true by default")
	}
	if cfg.BotReviewWait != 120*time.Second {
		t.Errorf("BotReviewWait = %v, want %v", cfg.BotReviewWait, 120*time.Second)
	}
	if cfg.BotMaxIterations != 3 {
		t.Errorf("BotMaxIterations = %d, want 3", cfg.BotMaxIterations)
	}
}

func TestLoadConfig_YoloMode(t *testing.T) {
	t.Setenv("HITL_MODE", "yolo")
	cfg := LoadConfig()

	if cfg.Mode != "yolo" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "yolo")
	}
	if cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be false in yolo mode")
	}
	if cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be false in yolo mode")
	}
}

func TestLoadConfig_CustomMode(t *testing.T) {
	t.Setenv("HITL_MODE", "custom")
	t.Setenv("HITL_GATE1_ENABLED", "false")
	t.Setenv("HITL_GATE3_ENABLED", "true")
	t.Setenv("HITL_GATE1_POLL_INTERVAL", "10m")
	t.Setenv("HITL_BOT_MAX_ITERATIONS", "5")

	cfg := LoadConfig()

	if cfg.Mode != "custom" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "custom")
	}
	if cfg.Gate1Enabled {
		t.Error("Gate1Enabled should be false")
	}
	if !cfg.Gate3Enabled {
		t.Error("Gate3Enabled should be true")
	}
	if cfg.Gate1PollInterval != 10*time.Minute {
		t.Errorf("Gate1PollInterval = %v, want %v", cfg.Gate1PollInterval, 10*time.Minute)
	}
	if cfg.BotMaxIterations != 5 {
		t.Errorf("BotMaxIterations = %d, want 5", cfg.BotMaxIterations)
	}
}

func TestLoadConfig_BotReviewers(t *testing.T) {
	t.Setenv("HITL_BOT_REVIEWERS", "@coderabbitai review,@greptile review")
	cfg := LoadConfig()

	if len(cfg.BotReviewers) != 2 {
		t.Fatalf("BotReviewers len = %d, want 2", len(cfg.BotReviewers))
	}
	if cfg.BotReviewers[0] != "@coderabbitai review" {
		t.Errorf("BotReviewers[0] = %q, want %q", cfg.BotReviewers[0], "@coderabbitai review")
	}
	if cfg.BotReviewers[1] != "@greptile review" {
		t.Errorf("BotReviewers[1] = %q, want %q", cfg.BotReviewers[1], "@greptile review")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hitl/ -v -run TestLoadConfig`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write the implementation**

```go
// internal/hitl/config.go
package hitl

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all HITL gate settings.
type Config struct {
	Mode               string        // full, yolo, custom
	Gate1Enabled       bool          // Gate 1: issue selection approval
	Gate1PollInterval  time.Duration // how often to poll for label changes
	Gate3Enabled       bool          // Gate 3: PR review with bot loop
	Gate3PollInterval  time.Duration // how often to poll for human PR action
	ResolveBotComments bool          // resolve threads after fixing bot comments
	BotReviewWait      time.Duration // time to wait for bot reviews after triggering
	BotMaxIterations   int           // max bot review → fix cycles
	BotReviewers       []string      // trigger comments to post (e.g., "@coderabbitai review")
}

// LoadConfig loads HITL configuration from environment variables.
func LoadConfig() Config {
	mode := envOrDefault("HITL_MODE", "full")

	cfg := Config{
		Mode:               mode,
		Gate1PollInterval:  envDurationOrDefault("HITL_GATE1_POLL_INTERVAL", 5*time.Minute),
		Gate3PollInterval:  envDurationOrDefault("HITL_GATE3_POLL_INTERVAL", 5*time.Minute),
		ResolveBotComments: envBoolOrDefault("HITL_RESOLVE_BOT_COMMENTS", true),
		BotReviewWait:      envDurationOrDefault("HITL_BOT_REVIEW_WAIT", 120*time.Second),
		BotMaxIterations:   envIntOrDefault("HITL_BOT_MAX_ITERATIONS", 3),
		BotReviewers:       envListOrDefault("HITL_BOT_REVIEWERS", []string{"@coderabbitai review", "@greptile review"}),
	}

	switch mode {
	case "yolo":
		cfg.Gate1Enabled = false
		cfg.Gate3Enabled = false
	case "custom":
		cfg.Gate1Enabled = envBoolOrDefault("HITL_GATE1_ENABLED", true)
		cfg.Gate3Enabled = envBoolOrDefault("HITL_GATE3_ENABLED", true)
	default: // "full"
		cfg.Gate1Enabled = true
		cfg.Gate3Enabled = true
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBoolOrDefault(key string, fallback bool) bool {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return fallback
	}
	return v
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

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return v
}

func envListOrDefault(key string, fallback []string) []string {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hitl/ -v -run TestLoadConfig`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/hitl/config.go internal/hitl/config_test.go
git commit -m "feat(hitl): add HITL configuration with mode support (#19)"
```

---

### Task 2: GitHub Adapter Label and Comment Methods

**Files:**
- Modify: `internal/github/adapter.go`
- Modify: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing tests for new adapter methods**

Add to `internal/github/adapter_test.go`:

```go
func TestAddLabel(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue edit"*"--add-label"*"agent:candidate"*)
    echo ""
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.AddLabel(context.Background(), 42, "agent:candidate"); err != nil {
		t.Fatalf("AddLabel() error: %v", err)
	}
}

func TestRemoveLabel(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue edit"*"--remove-label"*"agent:candidate"*)
    echo ""
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.RemoveLabel(context.Background(), 42, "agent:candidate"); err != nil {
		t.Fatalf("RemoveLabel() error: %v", err)
	}
}

func TestGetLabels(t *testing.T) {
	mockOutput := `[{"name":"bug"},{"name":"agent:candidate"}]`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	labels, err := a.GetLabels(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetLabels() error: %v", err)
	}

	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels[1] != "agent:candidate" {
		t.Errorf("labels[1] = %q, want %q", labels[1], "agent:candidate")
	}
}

func TestPostComment(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue comment"*"--body"*)
    echo "https://github.com/testowner/testrepo/issues/42#issuecomment-123"
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.PostComment(context.Background(), 42, "Hello world"); err != nil {
		t.Fatalf("PostComment() error: %v", err)
	}
}

func TestGetPRState(t *testing.T) {
	mockOutput := `{"state":"OPEN","isDraft":true,"reviewDecision":"REVIEW_REQUIRED"}`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	state, err := a.GetPRState(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetPRState() error: %v", err)
	}

	if state.State != "OPEN" {
		t.Errorf("State = %q, want %q", state.State, "OPEN")
	}
	if !state.IsDraft {
		t.Error("IsDraft should be true")
	}
	if state.ReviewDecision != "REVIEW_REQUIRED" {
		t.Errorf("ReviewDecision = %q, want %q", state.ReviewDecision, "REVIEW_REQUIRED")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github/ -v -run "TestAddLabel|TestRemoveLabel|TestGetLabels|TestPostComment|TestGetPRState"`
Expected: FAIL — methods not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/github/adapter.go`:

```go
// PRState represents the current state of a pull request.
type PRState struct {
	State          string `json:"state"`          // OPEN, CLOSED, MERGED
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
}

// AddLabel adds a label to an issue or PR.
func (a *Adapter) AddLabel(ctx context.Context, number int, label string) error {
	args := []string{
		"issue", "edit",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--add-label", label,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue edit --add-label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from an issue or PR.
func (a *Adapter) RemoveLabel(ctx context.Context, number int, label string) error {
	args := []string{
		"issue", "edit",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--remove-label", label,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue edit --remove-label: %w", err)
	}
	return nil
}

// GetLabels returns the label names on an issue or PR.
func (a *Adapter) GetLabels(ctx context.Context, number int) ([]string, error) {
	args := []string{
		"issue", "view",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--json", "labels",
		"--jq", ".labels",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue view labels: %w", err)
	}

	var labels []Label
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, fmt.Errorf("parsing labels: %w", err)
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names, nil
}

// PostComment posts a comment on an issue or PR.
func (a *Adapter) PostComment(ctx context.Context, number int, body string) error {
	args := []string{
		"issue", "comment",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--body", body,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue comment: %w", err)
	}
	return nil
}

// GetPRState returns the current state of a pull request.
func (a *Adapter) GetPRState(ctx context.Context, prNumber int) (*PRState, error) {
	args := []string{
		"pr", "view",
		fmt.Sprintf("%d", prNumber),
		"--repo", a.repo(),
		"--json", "state,isDraft,reviewDecision",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var state PRState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return nil, fmt.Errorf("parsing PR state: %w", err)
	}
	return &state, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/github/ -v -run "TestAddLabel|TestRemoveLabel|TestGetLabels|TestPostComment|TestGetPRState"`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): add label, comment, and PR state methods (#19)"
```

---

### Task 3: HITL Labels Package

**Files:**
- Create: `internal/hitl/labels.go`
- Create: `internal/hitl/labels_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/hitl/labels_test.go
package hitl

import (
	"context"
	"testing"
)

// mockAdapter implements the GHAdapter interface for testing.
type mockAdapter struct {
	labels       []string
	addedLabels  []string
	removedLabels []string
	comments     []string
	prState      *PRState
	addLabelErr  error
}

func (m *mockAdapter) AddLabel(_ context.Context, _ int, label string) error {
	if m.addLabelErr != nil {
		return m.addLabelErr
	}
	m.addedLabels = append(m.addedLabels, label)
	m.labels = append(m.labels, label)
	return nil
}

func (m *mockAdapter) RemoveLabel(_ context.Context, _ int, label string) error {
	m.removedLabels = append(m.removedLabels, label)
	return nil
}

func (m *mockAdapter) GetLabels(_ context.Context, _ int) ([]string, error) {
	return m.labels, nil
}

func (m *mockAdapter) PostComment(_ context.Context, _ int, body string) error {
	m.comments = append(m.comments, body)
	return nil
}

func (m *mockAdapter) GetPRState(_ context.Context, _ int) (*PRState, error) {
	return m.prState, nil
}

func TestHasLabel(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug", "agent:candidate"}}

	has, err := HasLabel(context.Background(), mock, 42, "agent:candidate")
	if err != nil {
		t.Fatalf("HasLabel() error: %v", err)
	}
	if !has {
		t.Error("HasLabel() = false, want true")
	}

	has, err = HasLabel(context.Background(), mock, 42, "agent:approved")
	if err != nil {
		t.Fatalf("HasLabel() error: %v", err)
	}
	if has {
		t.Error("HasLabel() = true, want false")
	}
}

func TestHasAnyLabel(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug", "agent:approved"}}

	found, err := HasAnyLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"})
	if err != nil {
		t.Fatalf("HasAnyLabel() error: %v", err)
	}
	if found != "agent:approved" {
		t.Errorf("HasAnyLabel() = %q, want %q", found, "agent:approved")
	}
}

func TestHasAnyLabel_NoneFound(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug"}}

	found, err := HasAnyLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"})
	if err != nil {
		t.Fatalf("HasAnyLabel() error: %v", err)
	}
	if found != "" {
		t.Errorf("HasAnyLabel() = %q, want empty string", found)
	}
}

func TestApplyLabel(t *testing.T) {
	mock := &mockAdapter{}

	if err := ApplyLabel(context.Background(), mock, 42, "agent:candidate"); err != nil {
		t.Fatalf("ApplyLabel() error: %v", err)
	}

	if len(mock.addedLabels) != 1 || mock.addedLabels[0] != "agent:candidate" {
		t.Errorf("addedLabels = %v, want [agent:candidate]", mock.addedLabels)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hitl/ -v -run "TestHasLabel|TestHasAnyLabel|TestApplyLabel"`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/hitl/labels.go
package hitl

import "context"

// GHAdapter defines the GitHub operations needed by HITL gates.
type GHAdapter interface {
	AddLabel(ctx context.Context, number int, label string) error
	RemoveLabel(ctx context.Context, number int, label string) error
	GetLabels(ctx context.Context, number int) ([]string, error)
	PostComment(ctx context.Context, number int, body string) error
	GetPRState(ctx context.Context, prNumber int) (*PRState, error)
}

// PRState represents the current state of a pull request.
// Mirrors github.PRState but defined here to avoid circular imports.
type PRState struct {
	State          string `json:"state"`          // OPEN, CLOSED, MERGED
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
}

// Label constants used by HITL gates.
const (
	LabelCandidate      = "agent:candidate"
	LabelApproved       = "agent:approved"
	LabelRejected       = "agent:rejected"
	LabelReadyForReview = "agent:ready-for-review"
)

// ApplyLabel adds a label to an issue or PR.
func ApplyLabel(ctx context.Context, gh GHAdapter, number int, label string) error {
	return gh.AddLabel(ctx, number, label)
}

// RemoveLabel removes a label from an issue or PR.
func RemoveLabel(ctx context.Context, gh GHAdapter, number int, label string) error {
	return gh.RemoveLabel(ctx, number, label)
}

// HasLabel checks if an issue or PR has a specific label.
func HasLabel(ctx context.Context, gh GHAdapter, number int, label string) (bool, error) {
	labels, err := gh.GetLabels(ctx, number)
	if err != nil {
		return false, err
	}
	for _, l := range labels {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}

// HasAnyLabel checks if an issue or PR has any of the given labels.
// Returns the first matching label found, or empty string if none match.
func HasAnyLabel(ctx context.Context, gh GHAdapter, number int, targets []string) (string, error) {
	labels, err := gh.GetLabels(ctx, number)
	if err != nil {
		return "", err
	}
	labelSet := make(map[string]bool, len(labels))
	for _, l := range labels {
		labelSet[l] = true
	}
	for _, target := range targets {
		if labelSet[target] {
			return target, nil
		}
	}
	return "", nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hitl/ -v -run "TestHasLabel|TestHasAnyLabel|TestApplyLabel"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hitl/labels.go internal/hitl/labels_test.go
git commit -m "feat(hitl): add label operations and GHAdapter interface (#19)"
```

---

### Task 4: HITL Gates — WaitForLabel and WaitForPRAction

**Files:**
- Create: `internal/hitl/gates.go`
- Create: `internal/hitl/gates_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/hitl/gates_test.go
package hitl

import (
	"context"
	"testing"
	"time"
)

// pollCountAdapter tracks how many times GetLabels/GetPRState were called.
type pollCountAdapter struct {
	mockAdapter
	labelPollCount int
	prPollCount    int
	// labelsSequence returns different labels on each call
	labelsSequence [][]string
	prSequence     []*PRState
}

func (p *pollCountAdapter) GetLabels(_ context.Context, _ int) ([]string, error) {
	idx := p.labelPollCount
	p.labelPollCount++
	if idx < len(p.labelsSequence) {
		return p.labelsSequence[idx], nil
	}
	return p.labelsSequence[len(p.labelsSequence)-1], nil
}

func (p *pollCountAdapter) GetPRState(_ context.Context, _ int) (*PRState, error) {
	idx := p.prPollCount
	p.prPollCount++
	if idx < len(p.prSequence) {
		return p.prSequence[idx], nil
	}
	return p.prSequence[len(p.prSequence)-1], nil
}

func TestWaitForLabel_ImmediateMatch(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{
			{"bug", "agent:approved"},
		},
	}

	label, err := WaitForLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"}, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForLabel() error: %v", err)
	}
	if label != "agent:approved" {
		t.Errorf("WaitForLabel() = %q, want %q", label, "agent:approved")
	}
	if mock.labelPollCount != 1 {
		t.Errorf("polled %d times, want 1", mock.labelPollCount)
	}
}

func TestWaitForLabel_PollsUntilMatch(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{
			{"bug"},
			{"bug"},
			{"bug", "agent:rejected"},
		},
	}

	label, err := WaitForLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"}, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForLabel() error: %v", err)
	}
	if label != "agent:rejected" {
		t.Errorf("WaitForLabel() = %q, want %q", label, "agent:rejected")
	}
	if mock.labelPollCount != 3 {
		t.Errorf("polled %d times, want 3", mock.labelPollCount)
	}
}

func TestWaitForLabel_ContextCancelled(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{{"bug"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := WaitForLabel(ctx, mock, 42, []string{"agent:approved"}, 10*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForLabel() should return error on context cancellation")
	}
}

func TestWaitForPRAction_Merged(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{
			{State: "MERGED", IsDraft: false},
		},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "merged" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "merged")
	}
}

func TestWaitForPRAction_Closed(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{
			{State: "CLOSED", IsDraft: false},
		},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "closed" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "closed")
	}
}

func TestWaitForPRAction_ChangesRequested(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{
			{State: "OPEN", IsDraft: true, ReviewDecision: "REVIEW_REQUIRED"},
			{State: "OPEN", IsDraft: true, ReviewDecision: "CHANGES_REQUESTED"},
		},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "changes_requested" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "changes_requested")
	}
}

func TestWaitForPRAction_Approved(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{
			{State: "OPEN", IsDraft: true, ReviewDecision: "APPROVED"},
		},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "approved" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "approved")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hitl/ -v -run "TestWaitFor"`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/hitl/gates.go
package hitl

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WaitForLabel polls for any of the target labels on an issue.
// Returns the first matching label found. Blocks until a match or context cancellation.
func WaitForLabel(ctx context.Context, gh GHAdapter, issueNumber int, targetLabels []string, pollInterval time.Duration) (string, error) {
	for {
		found, err := HasAnyLabel(ctx, gh, issueNumber, targetLabels)
		if err != nil {
			return "", fmt.Errorf("checking labels on issue #%d: %w", issueNumber, err)
		}
		if found != "" {
			return found, nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for label on issue #%d: %w", issueNumber, ctx.Err())
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

// WaitForPRAction polls the PR state until a terminal action is detected.
// Returns one of: "merged", "closed", "approved", "changes_requested".
// Blocks until an action or context cancellation.
func WaitForPRAction(ctx context.Context, gh GHAdapter, prNumber int, pollInterval time.Duration) (string, error) {
	for {
		state, err := gh.GetPRState(ctx, prNumber)
		if err != nil {
			return "", fmt.Errorf("checking PR #%d state: %w", prNumber, err)
		}

		action := classifyPRAction(state)
		if action != "" {
			return action, nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for PR #%d action: %w", prNumber, ctx.Err())
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

// classifyPRAction returns a terminal action string from PR state, or empty if no action yet.
func classifyPRAction(state *PRState) string {
	switch strings.ToUpper(state.State) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}

	switch strings.ToUpper(state.ReviewDecision) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes_requested"
	}

	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hitl/ -v -run "TestWaitFor"`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/hitl/gates.go internal/hitl/gates_test.go
git commit -m "feat(hitl): add WaitForLabel and WaitForPRAction polling gates (#19)"
```

---

### Task 5: HITL Comments — Bot Review Triggering and Thread Resolution

**Files:**
- Create: `internal/hitl/comments.go`
- Create: `internal/hitl/comments_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/hitl/comments_test.go
package hitl

import (
	"context"
	"testing"
)

func TestTriggerBotReviews(t *testing.T) {
	mock := &mockAdapter{}

	triggers := []string{"@coderabbitai review", "@greptile review"}
	if err := TriggerBotReviews(context.Background(), mock, 42, triggers); err != nil {
		t.Fatalf("TriggerBotReviews() error: %v", err)
	}

	if len(mock.comments) != 2 {
		t.Fatalf("posted %d comments, want 2", len(mock.comments))
	}
	if mock.comments[0] != "@coderabbitai review" {
		t.Errorf("comments[0] = %q, want %q", mock.comments[0], "@coderabbitai review")
	}
	if mock.comments[1] != "@greptile review" {
		t.Errorf("comments[1] = %q, want %q", mock.comments[1], "@greptile review")
	}
}

func TestPostTriageRationale(t *testing.T) {
	mock := &mockAdapter{}

	if err := PostTriageRationale(context.Background(), mock, 42, "L1", "low", 85, "Simple doc fix"); err != nil {
		t.Fatalf("PostTriageRationale() error: %v", err)
	}

	if len(mock.comments) != 1 {
		t.Fatalf("posted %d comments, want 1", len(mock.comments))
	}

	comment := mock.comments[0]
	if !contains(comment, "Agent Triage") {
		t.Error("comment should contain 'Agent Triage'")
	}
	if !contains(comment, "L1") {
		t.Error("comment should contain difficulty")
	}
	if !contains(comment, "low") {
		t.Error("comment should contain blast radius")
	}
	if !contains(comment, "85") {
		t.Error("comment should contain score")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hitl/ -v -run "TestTriggerBotReviews|TestPostTriageRationale"`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/hitl/comments.go
package hitl

import (
	"context"
	"fmt"
)

// TriggerBotReviews posts each trigger comment on a PR to invoke bot reviewers.
func TriggerBotReviews(ctx context.Context, gh GHAdapter, prNumber int, triggers []string) error {
	for _, trigger := range triggers {
		if err := gh.PostComment(ctx, prNumber, trigger); err != nil {
			return fmt.Errorf("posting trigger %q on PR #%d: %w", trigger, prNumber, err)
		}
	}
	return nil
}

// PostTriageRationale posts a comment on an issue explaining why it was selected.
func PostTriageRationale(ctx context.Context, gh GHAdapter, issueNumber int, difficulty, blastRadius string, score int, rationale string) error {
	body := fmt.Sprintf(`### 🤖 Agent Triage

This issue has been selected as a candidate for automated implementation.

| Property | Value |
|----------|-------|
| **Difficulty** | %s |
| **Blast Radius** | %s |
| **Score** | %d |

**Rationale:** %s

---
To approve, add the `+"`agent:approved`"+` label. To reject, add the `+"`agent:rejected`"+` label.`,
		difficulty, blastRadius, score, rationale)

	return gh.PostComment(ctx, issueNumber, body)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hitl/ -v -run "TestTriggerBotReviews|TestPostTriageRationale"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hitl/comments.go internal/hitl/comments_test.go
git commit -m "feat(hitl): add bot review triggering and triage rationale comments (#19)"
```

---

### Task 6: Thread Resolution via GitHub API

**Files:**
- Modify: `internal/github/adapter.go`
- Modify: `internal/github/adapter_test.go`
- Modify: `internal/hitl/labels.go` (add to GHAdapter interface)

- [ ] **Step 1: Write the failing test for ResolveThread**

Add to `internal/github/adapter_test.go`:

```go
func TestGetReviewThreads(t *testing.T) {
	mockOutput := `{
		"data": {
			"repository": {
				"pullRequest": {
					"reviewThreads": {
						"nodes": [
							{"id": "RT_1", "isResolved": false, "comments": {"nodes": [{"body": "Fix this"}]}},
							{"id": "RT_2", "isResolved": true, "comments": {"nodes": [{"body": "Already fixed"}]}}
						]
					}
				}
			}
		}
	}`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\ncat <<'ENDOFOUTPUT'\n"+mockOutput+"\nENDOFOUTPUT\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	threads, err := a.GetReviewThreads(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetReviewThreads() error: %v", err)
	}

	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	if threads[0].ID != "RT_1" {
		t.Errorf("threads[0].ID = %q, want %q", threads[0].ID, "RT_1")
	}
	if threads[0].IsResolved {
		t.Error("threads[0] should not be resolved")
	}
	if threads[1].IsResolved != true {
		t.Error("threads[1] should be resolved")
	}
}

func TestResolveThread(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"graphql"*)
    echo '{"data":{"resolveReviewThread":{"thread":{"id":"RT_1"}}}}'
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.ResolveThread(context.Background(), "RT_1"); err != nil {
		t.Fatalf("ResolveThread() error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github/ -v -run "TestGetReviewThreads|TestResolveThread"`
Expected: FAIL — methods not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/github/adapter.go`:

```go
// ReviewThread represents a review thread on a PR.
type ReviewThread struct {
	ID         string `json:"id"`
	IsResolved bool   `json:"isResolved"`
	Body       string // first comment body
}

// reviewThreadsResponse is the GraphQL response for review threads.
type reviewThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						Comments   struct {
							Nodes []struct {
								Body string `json:"body"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// GetReviewThreads returns all review threads on a PR using the GraphQL API.
func (a *Adapter) GetReviewThreads(ctx context.Context, prNumber int) ([]ReviewThread, error) {
	query := fmt.Sprintf(`query {
		repository(owner: %q, name: %q) {
			pullRequest(number: %d) {
				reviewThreads(first: 100) {
					nodes {
						id
						isResolved
						comments(first: 1) {
							nodes { body }
						}
					}
				}
			}
		}
	}`, a.Owner, a.Repo, prNumber)

	out, err := a.runGH(ctx, "api", "graphql", "-f", "query="+query)
	if err != nil {
		return nil, fmt.Errorf("gh api graphql (review threads): %w", err)
	}

	var resp reviewThreadsResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parsing review threads: %w", err)
	}

	nodes := resp.Data.Repository.PullRequest.ReviewThreads.Nodes
	threads := make([]ReviewThread, len(nodes))
	for i, n := range nodes {
		body := ""
		if len(n.Comments.Nodes) > 0 {
			body = n.Comments.Nodes[0].Body
		}
		threads[i] = ReviewThread{
			ID:         n.ID,
			IsResolved: n.IsResolved,
			Body:       body,
		}
	}
	return threads, nil
}

// ResolveThread resolves a review thread by its node ID using the GraphQL API.
func (a *Adapter) ResolveThread(ctx context.Context, threadID string) error {
	mutation := fmt.Sprintf(`mutation {
		resolveReviewThread(input: {threadId: %q}) {
			thread { id }
		}
	}`, threadID)

	_, err := a.runGH(ctx, "api", "graphql", "-f", "query="+mutation)
	if err != nil {
		return fmt.Errorf("gh api graphql (resolve thread): %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Update the GHAdapter interface to include thread operations**

Add to `internal/hitl/labels.go` GHAdapter interface:

```go
	GetReviewThreads(ctx context.Context, prNumber int) ([]ReviewThread, error)
	ResolveThread(ctx context.Context, threadID string) error
```

Add the `ReviewThread` type to `internal/hitl/labels.go`:

```go
// ReviewThread represents a review thread on a PR.
type ReviewThread struct {
	ID         string
	IsResolved bool
	Body       string
}
```

Update the mockAdapter in `internal/hitl/labels_test.go` to satisfy the updated interface:

```go
func (m *mockAdapter) GetReviewThreads(_ context.Context, _ int) ([]ReviewThread, error) {
	return m.threads, nil
}

func (m *mockAdapter) ResolveThread(_ context.Context, threadID string) error {
	m.resolvedThreads = append(m.resolvedThreads, threadID)
	return nil
}
```

Add fields to mockAdapter:
```go
	threads         []ReviewThread
	resolvedThreads []string
```

- [ ] **Step 5: Run all tests to verify they pass**

Run: `go test ./internal/github/ -v -run "TestGetReviewThreads|TestResolveThread" && go test ./internal/hitl/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go internal/hitl/labels.go internal/hitl/labels_test.go
git commit -m "feat(hitl): add review thread resolution via GraphQL API (#19)"
```

---

### Task 7: HITL Thread Resolution Logic

**Files:**
- Modify: `internal/hitl/comments.go`
- Modify: `internal/hitl/comments_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/hitl/comments_test.go`:

```go
func TestResolveAddressedThreads(t *testing.T) {
	mock := &mockAdapter{
		threads: []ReviewThread{
			{ID: "RT_1", IsResolved: false, Body: "Fix this typo"},
			{ID: "RT_2", IsResolved: true, Body: "Already resolved"},
			{ID: "RT_3", IsResolved: false, Body: "Another issue"},
		},
	}

	resolved, err := ResolveAddressedThreads(context.Background(), mock, 42)
	if err != nil {
		t.Fatalf("ResolveAddressedThreads() error: %v", err)
	}

	// Should resolve all unresolved threads (RT_1, RT_3)
	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}
	if len(mock.resolvedThreads) != 2 {
		t.Fatalf("resolvedThreads = %v, want [RT_1, RT_3]", mock.resolvedThreads)
	}
}

func TestResolveAddressedThreads_NoneUnresolved(t *testing.T) {
	mock := &mockAdapter{
		threads: []ReviewThread{
			{ID: "RT_1", IsResolved: true, Body: "Done"},
		},
	}

	resolved, err := ResolveAddressedThreads(context.Background(), mock, 42)
	if err != nil {
		t.Fatalf("ResolveAddressedThreads() error: %v", err)
	}
	if resolved != 0 {
		t.Errorf("resolved = %d, want 0", resolved)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hitl/ -v -run "TestResolveAddressedThreads"`
Expected: FAIL — function not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/hitl/comments.go`:

```go
// ResolveAddressedThreads resolves all unresolved review threads on a PR.
// Returns the number of threads resolved.
func ResolveAddressedThreads(ctx context.Context, gh GHAdapter, prNumber int) (int, error) {
	threads, err := gh.GetReviewThreads(ctx, prNumber)
	if err != nil {
		return 0, fmt.Errorf("fetching review threads for PR #%d: %w", prNumber, err)
	}

	resolved := 0
	for _, t := range threads {
		if t.IsResolved {
			continue
		}
		if err := gh.ResolveThread(ctx, t.ID); err != nil {
			return resolved, fmt.Errorf("resolving thread %s: %w", t.ID, err)
		}
		resolved++
	}
	return resolved, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hitl/ -v -run "TestResolveAddressedThreads"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/hitl/comments.go internal/hitl/comments_test.go
git commit -m "feat(hitl): add bulk thread resolution for bot review cleanup (#19)"
```

---

### Task 8: Integrate Gate 1 into Implementer CLI

**Files:**
- Modify: `cmd/implementer/main.go`

- [ ] **Step 1: Write the Gate 1 integration code**

Add the HITL import and Gate 1 logic to `cmd/implementer/main.go`. Insert after issue selection (line 62) and before fetching full issue details (line 64):

Add import:
```go
	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
```

Insert Gate 1 logic after `log.Printf("Selected issue #%d: %s (score %d)", ...)` (after line 62) and before `// 2. Fetch full issue details` (line 64):

```go
	// 1b. Gate 1: Issue selection approval (HITL)
	hitlCfg := hitl.LoadConfig()
	if hitlCfg.Gate1Enabled {
		log.Printf("[HITL] Gate 1 active — requesting approval for issue #%d", issue.Number)

		// Apply candidate label
		if err := adapter.AddLabel(ctx, issue.Number, hitl.LabelCandidate); err != nil {
			log.Printf("[HITL] Warning: failed to apply candidate label: %v", err)
		}

		// Post triage rationale
		if err := hitl.PostTriageRationale(ctx, adapter, issue.Number, issue.Difficulty, issue.BlastRadius, issue.Score, issue.Rationale); err != nil {
			log.Printf("[HITL] Warning: failed to post rationale: %v", err)
		}

		// Wait for human decision
		log.Printf("[HITL] Waiting for %s or %s label on issue #%d (polling every %v)...",
			hitl.LabelApproved, hitl.LabelRejected, issue.Number, hitlCfg.Gate1PollInterval)

		label, err := hitl.WaitForLabel(ctx, adapter, issue.Number,
			[]string{hitl.LabelApproved, hitl.LabelRejected}, hitlCfg.Gate1PollInterval)
		if err != nil {
			log.Fatalf("[HITL] Gate 1 error: %v", err)
		}

		if label == hitl.LabelRejected {
			log.Printf("[HITL] Issue #%d rejected by human, exiting", issue.Number)
			os.Exit(0)
		}
		log.Printf("[HITL] Issue #%d approved by human, proceeding", issue.Number)
	} else {
		log.Printf("[HITL] Gate 1 disabled (mode=%s), proceeding automatically", hitlCfg.Mode)
	}
```

- [ ] **Step 2: Verify the triage.RankedIssue struct has the required fields**

Check that `triage.RankedIssue` has `Difficulty`, `BlastRadius`, `Score`, and `Rationale` fields. If any are missing, the compiler will catch it — adjust field names to match the actual struct.

Run: `go build ./cmd/implementer/`
Expected: BUILD SUCCESS (or field name adjustments needed)

- [ ] **Step 3: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(hitl): integrate Gate 1 (issue approval) into implementer CLI (#19)"
```

---

### Task 9: Integrate Gate 3 into Implementer CLI

**Files:**
- Modify: `cmd/implementer/main.go`

- [ ] **Step 1: Write the Gate 3 integration code**

Add the Gate 3 bot review loop after PR creation. Replace the final `log.Printf("Draft PR created: %s", prURL)` and add the Gate 3 logic:

```go
	log.Printf("Draft PR created: %s", prURL)

	// 10. Gate 3: Bot review loop + human approval (HITL)
	if hitlCfg.Gate3Enabled {
		log.Printf("[HITL] Gate 3 active — starting bot review loop on PR %s", prURL)

		// Extract PR number from URL (last path segment)
		prNum := extractPRNumber(prURL)
		if prNum == 0 {
			log.Fatalf("[HITL] could not extract PR number from URL: %s", prURL)
		}

		for botIter := 1; botIter <= hitlCfg.BotMaxIterations; botIter++ {
			log.Printf("[HITL] Bot review iteration %d/%d", botIter, hitlCfg.BotMaxIterations)

			// Trigger bot reviews
			if err := hitl.TriggerBotReviews(ctx, adapter, prNum, hitlCfg.BotReviewers); err != nil {
				log.Printf("[HITL] Warning: failed to trigger bot reviews: %v", err)
			}

			// Wait for bot reviews to arrive
			log.Printf("[HITL] Waiting %v for bot reviews...", hitlCfg.BotReviewWait)
			time.Sleep(hitlCfg.BotReviewWait)

			// Run responder to fix bot comments
			commentData, err := fetchPRComments(ctx, adapter, prNum)
			if err != nil {
				log.Printf("[HITL] Warning: failed to fetch PR comments: %v", err)
				continue
			}

			comments, err := responder.ParseInlineComments(commentData)
			if err != nil {
				log.Printf("[HITL] Warning: failed to parse comments: %v", err)
				continue
			}

			actionable := responder.Classify(comments)
			if len(actionable) == 0 {
				log.Printf("[HITL] No actionable bot comments, bot loop complete")
				break
			}

			log.Printf("[HITL] Found %d actionable comments, running fix agent...", len(actionable))
			prompt := responder.BuildFixPrompt(actionable)
			fixPlan := &planner.ImplementationPlan{Markdown: prompt}

			fixResult, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, fixPlan, maxIter, implMaxCost)
			if err != nil {
				log.Printf("[HITL] Fix agent failed: %v", err)
				continue
			}
			log.Printf("[HITL] Fix agent completed in %d iterations", fixResult.Iterations)

			// Check for changes and push
			statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
			statusCmd.Dir = repoDir
			statusOutput, err := statusCmd.Output()
			if err != nil || len(statusOutput) == 0 {
				log.Printf("[HITL] No changes from fix agent")
				break
			}

			commitMsg := fmt.Sprintf("fix: address bot review comments (iteration %d)", botIter)
			pushCmd := [][]string{
				{"git", "add", "-A"},
				{"git", "commit", "-m", commitMsg},
				{"git", "push", "origin", branch},
			}
			for _, args := range pushCmd {
				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				cmd.Dir = repoDir
				if out, err := cmd.CombinedOutput(); err != nil {
					log.Printf("[HITL] %s failed: %v\n%s", args[0], err, out)
					break
				}
			}

			// Resolve addressed threads
			if hitlCfg.ResolveBotComments {
				resolved, err := hitl.ResolveAddressedThreads(ctx, adapter, prNum)
				if err != nil {
					log.Printf("[HITL] Warning: failed to resolve threads: %v", err)
				} else {
					log.Printf("[HITL] Resolved %d review threads", resolved)
				}
			}
		}

		// Signal human
		if err := adapter.AddLabel(ctx, prNum, hitl.LabelReadyForReview); err != nil {
			log.Printf("[HITL] Warning: failed to apply ready-for-review label: %v", err)
		}
		log.Printf("[HITL] Bot review loop complete. Waiting for human action on PR #%d...", prNum)

		// Wait for human decision
		action, err := hitl.WaitForPRAction(ctx, adapter, prNum, hitlCfg.Gate3PollInterval)
		if err != nil {
			log.Fatalf("[HITL] Gate 3 error: %v", err)
		}

		switch action {
		case "merged", "approved":
			log.Printf("[HITL] PR #%d %s by human", prNum, action)
		case "changes_requested":
			log.Printf("[HITL] PR #%d has changes requested — run `make respond RESPONDER_PR_NUMBER=%d` to address", prNum, prNum)
		case "closed":
			log.Printf("[HITL] PR #%d closed by human", prNum)
		}
	}
```

- [ ] **Step 2: Add the extractPRNumber helper and required imports**

Add to `cmd/implementer/main.go`:

```go
import (
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/responder"
)
```

Add helper function:

```go
func extractPRNumber(prURL string) int {
	parts := strings.Split(prURL, "/")
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}
```

Also add the `strings` import if not already present.

- [ ] **Step 3: Add fetchPRComments helper (reuse from responder CLI)**

Since `cmd/implementer/main.go` now needs `fetchPRComments`, add it as a local helper (same implementation as in `cmd/responder/main.go`):

```go
func fetchPRComments(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"api",
		fmt.Sprintf("repos/%s/%s/pulls/%d/comments", adapter.Owner, adapter.Repo, prNum),
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w\n%s", err, out)
	}
	return out, nil
}
```

- [ ] **Step 4: Verify the build compiles**

Run: `go build ./cmd/implementer/`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(hitl): integrate Gate 3 (bot review loop + human wait) into implementer CLI (#19)"
```

---

### Task 10: Add Thread Resolution to Responder CLI

**Files:**
- Modify: `cmd/responder/main.go`

- [ ] **Step 1: Add thread resolution and bot re-triggering to the responder loop**

Add import:
```go
	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
```

After the `commitAndPush` call (around line 138), add thread resolution and bot re-triggering:

```go
		// Resolve addressed threads (HITL)
		hitlCfg := hitl.LoadConfig()
		if hitlCfg.ResolveBotComments {
			resolved, resolveErr := hitl.ResolveAddressedThreads(ctx, adapter, prNum)
			if resolveErr != nil {
				log.Printf("Warning: failed to resolve threads: %v", resolveErr)
			} else if resolved > 0 {
				log.Printf("Resolved %d review threads", resolved)
			}
		}

		// Re-trigger bot reviews after pushing fixes
		if len(hitlCfg.BotReviewers) > 0 {
			if triggerErr := hitl.TriggerBotReviews(ctx, adapter, prNum, hitlCfg.BotReviewers); triggerErr != nil {
				log.Printf("Warning: failed to re-trigger bot reviews: %v", triggerErr)
			} else {
				log.Printf("Re-triggered bot reviews")
			}
		}
```

- [ ] **Step 2: Verify the build compiles**

Run: `go build ./cmd/responder/`
Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add cmd/responder/main.go
git commit -m "feat(hitl): add thread resolution and bot re-triggering to responder (#19)"
```

---

### Task 11: Make github.Adapter Satisfy hitl.GHAdapter Interface

**Files:**
- Create: `internal/github/hitl_adapter.go`
- Create: `internal/github/hitl_adapter_test.go`

The `github.Adapter` already has `AddLabel`, `RemoveLabel`, `GetLabels`, `PostComment`, `GetPRState`, `GetReviewThreads`, and `ResolveThread` methods. We need to ensure the types align with `hitl.GHAdapter`. Since `hitl.PRState` and `hitl.ReviewThread` are separate types from `github.PRState` and `github.ReviewThread`, we need a thin wrapper.

- [ ] **Step 1: Write the failing test**

```go
// internal/github/hitl_adapter_test.go
package github

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
)

func TestAdapterSatisfiesGHAdapter(t *testing.T) {
	// Compile-time check that HITLAdapter satisfies hitl.GHAdapter
	var _ hitl.GHAdapter = (*HITLAdapter)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/ -v -run TestAdapterSatisfiesGHAdapter`
Expected: FAIL — HITLAdapter not defined

- [ ] **Step 3: Write the wrapper**

```go
// internal/github/hitl_adapter.go
package github

import (
	"context"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
)

// HITLAdapter wraps Adapter to satisfy the hitl.GHAdapter interface.
type HITLAdapter struct {
	*Adapter
}

// GetLabels wraps Adapter.GetLabels (already returns []string).
// (Adapter.GetLabels already matches the interface signature — included for clarity.)

// GetPRState wraps Adapter.GetPRState, converting github.PRState to hitl.PRState.
func (h *HITLAdapter) GetPRState(ctx context.Context, prNumber int) (*hitl.PRState, error) {
	state, err := h.Adapter.GetPRState(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	return &hitl.PRState{
		State:          state.State,
		IsDraft:        state.IsDraft,
		ReviewDecision: state.ReviewDecision,
	}, nil
}

// GetReviewThreads wraps Adapter.GetReviewThreads, converting types.
func (h *HITLAdapter) GetReviewThreads(ctx context.Context, prNumber int) ([]hitl.ReviewThread, error) {
	threads, err := h.Adapter.GetReviewThreads(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	result := make([]hitl.ReviewThread, len(threads))
	for i, t := range threads {
		result[i] = hitl.ReviewThread{
			ID:         t.ID,
			IsResolved: t.IsResolved,
			Body:       t.Body,
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/ -v -run TestAdapterSatisfiesGHAdapter`
Expected: PASS

- [ ] **Step 5: Update `cmd/implementer/main.go` to use HITLAdapter**

Where the `adapter` variable is used for HITL calls, wrap it:

```go
hitlAdapter := &github.HITLAdapter{Adapter: adapter}
```

Then use `hitlAdapter` instead of `adapter` in all `hitl.*` function calls.

Run: `go build ./cmd/implementer/`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add internal/github/hitl_adapter.go internal/github/hitl_adapter_test.go cmd/implementer/main.go
git commit -m "feat(hitl): add HITLAdapter wrapper for github.Adapter (#19)"
```

---

### Task 12: End-to-End Verification and Documentation

**Files:**
- Modify: `docs/specs/2026-04-08-hitl-integration-design.md` (add implementation notes)

- [ ] **Step 1: Run all tests**

Run: `go test ./internal/hitl/ ./internal/github/ -v`
Expected: ALL PASS

- [ ] **Step 2: Build all CLIs**

Run: `go build ./cmd/implementer/ && go build ./cmd/responder/`
Expected: BUILD SUCCESS for both

- [ ] **Step 3: Verify HITL mode defaults**

Run: `HITL_MODE=yolo go run ./cmd/implementer/ 2>&1 | head -5`
Expected: Should show `[HITL] Gate 1 disabled (mode=yolo)` in output (will fail on missing API keys, but the HITL log line confirms config loading works)

- [ ] **Step 4: Update design spec with implementation notes**

Add to bottom of `docs/specs/2026-04-08-hitl-integration-design.md`:

```markdown
## Implementation Notes

**Completed:** 2026-04-08

### Package Structure
- `internal/hitl/config.go` — Config struct + LoadConfig() from env vars
- `internal/hitl/labels.go` — GHAdapter interface + label operations
- `internal/hitl/gates.go` — WaitForLabel + WaitForPRAction polling loops
- `internal/hitl/comments.go` — Bot review triggering, triage rationale, thread resolution
- `internal/github/hitl_adapter.go` — HITLAdapter wrapper for github.Adapter → hitl.GHAdapter

### Environment Variables
All `HITL_*` env vars documented in the Configuration sections above.

### Gate 2 Deferred
See issue #26 for future plan approval gate implementation.
```

- [ ] **Step 5: Commit**

```bash
git add docs/specs/2026-04-08-hitl-integration-design.md
git commit -m "docs: add implementation notes to HITL design spec (#19)"
```

- [ ] **Step 6: Final full test run**

Run: `go test ./... 2>&1 | tail -20`
Expected: All packages pass (existing tests unbroken)
