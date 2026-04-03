# Milestone 1: Low-Risk Task Loop — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the M0 CLI with an Archivist (LLM-enhanced dossier), Triage (policy-based accept/reject), Verifier (command execution in target repo), and orchestrator workflow that chains them together.

**Architecture:** The `run` command delegates to `orchestrator.RunWorkflow()` which chains: keyword dossier -> Archivist LLM enhancement -> Triage decision -> Verifier command execution -> report. LLM calls go through a thin client wrapping the `openai-go` SDK pointed at Gemini's OpenAI-compatible endpoint.

**Tech Stack:** Go 1.24, openai-go SDK, cobra, viper, os/exec for command execution, git worktrees for isolation

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/llm/client.go` | Create | Thin wrapper around openai-go: `Complete(ctx, system, user) (string, error)` |
| `internal/llm/client_test.go` | Create | Test with mock HTTP server |
| `internal/config/config.go` | Modify | Add `LoadModels()` and `ModelsConfig` types |
| `internal/config/config_test.go` | Modify | Test models config loading |
| `internal/models/run.go` | Modify | Add `LLMCall`, triage/verifier fields to `Run` |
| `internal/agents/archivist.go` | Rewrite | LLM-enhanced dossier with fallback |
| `internal/agents/archivist_test.go` | Create | Test prompt construction and JSON parsing |
| `internal/agents/triage.go` | Rewrite | Policy-based accept/reject/defer |
| `internal/agents/triage_test.go` | Create | Test decision logic |
| `internal/agents/verifier.go` | Rewrite | Run commands, produce VerifierReport |
| `internal/agents/verifier_test.go` | Create | Test with simple commands |
| `internal/execution/command_runner.go` | Rewrite | Exec with timeout, output capture, worktree |
| `internal/execution/command_runner_test.go` | Create | Test timeout, capture, worktree |
| `internal/orchestrator/workflow.go` | Rewrite | Chain stages, produce WorkflowResult |
| `internal/orchestrator/workflow_test.go` | Create | Integration test with mock LLM + temp repo |
| `internal/reporting/markdown_report.go` | Modify | Add triage, verifier, LLM sections |
| `internal/reporting/markdown_report_test.go` | Modify | Test new sections |
| `cmd/experiment/main.go` | Modify | Delegate to RunWorkflow |
| `configs/models.yaml` | Modify | Add provider.base_url |
| `.env.example` | Modify | Add GEMINI_API_KEY |

---

### Task 1: Add openai-go dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/openai/openai-go@latest
```

- [ ] **Step 2: Tidy**

```bash
go mod tidy
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add openai-go SDK"
```

---

### Task 2: Models config loading

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoadModels(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.yaml")
	content := []byte(`provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"

roles:
  archivist:
    model: "gemini-2.5-flash"
  triage:
    model: "gemini-2.5-flash"
`)
	if err := os.WriteFile(modelsPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	mcfg, err := LoadModels(modelsPath)
	if err != nil {
		t.Fatalf("LoadModels() error: %v", err)
	}
	if mcfg.Provider.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai/" {
		t.Errorf("BaseURL = %q, want Gemini endpoint", mcfg.Provider.BaseURL)
	}
	if mcfg.Roles["archivist"].Model != "gemini-2.5-flash" {
		t.Errorf("archivist model = %q, want gemini-2.5-flash", mcfg.Roles["archivist"].Model)
	}
}

func TestLoadModelsAPIKeyFromEnv(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.yaml")
	content := []byte(`provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"
roles:
  archivist:
    model: "gemini-2.5-flash"
`)
	if err := os.WriteFile(modelsPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GEMINI_API_KEY", "test-key-123")

	mcfg, err := LoadModels(modelsPath)
	if err != nil {
		t.Fatalf("LoadModels() error: %v", err)
	}
	if mcfg.APIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want test-key-123", mcfg.APIKey)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v -run TestLoadModels
```

Expected: FAIL (LoadModels not defined).

- [ ] **Step 3: Write implementation**

Add to `internal/config/config.go`:

```go
// ModelsConfig holds LLM provider and per-role model configuration.
type ModelsConfig struct {
	Provider ProviderConfig        `mapstructure:"provider"`
	Roles    map[string]RoleConfig `mapstructure:"roles"`
	APIKey   string                // populated from env, not from file
}

type ProviderConfig struct {
	BaseURL string `mapstructure:"base_url"`
}

type RoleConfig struct {
	Model string `mapstructure:"model"`
}

// LoadModels reads the models config file and applies env var for API key.
func LoadModels(path string) (ModelsConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return ModelsConfig{}, fmt.Errorf("reading models config %s: %w", path, err)
	}

	var mcfg ModelsConfig
	if err := v.Unmarshal(&mcfg); err != nil {
		return ModelsConfig{}, fmt.Errorf("unmarshalling models config: %w", err)
	}

	mcfg.APIKey = os.Getenv("GEMINI_API_KEY")

	return mcfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/ -v -run TestLoadModels
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add models config loading with API key from env"
```

---

### Task 3: LLM client

**Files:**
- Create: `internal/llm/client.go`
- Create: `internal/llm/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/llm/client_test.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gemini-2.5-flash",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from the LLM",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gemini-2.5-flash")
	result, err := client.Complete(context.Background(), "You are a helper.", "Say hello")
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result != "Hello from the LLM" {
		t.Errorf("result = %q, want 'Hello from the LLM'", result)
	}
}

func TestCompleteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "server error"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gemini-2.5-flash")
	_, err := client.Complete(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/llm/ -v
```

Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Write implementation**

Create `internal/llm/client.go`:

```go
package llm

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Client wraps the OpenAI-compatible API for LLM completions.
type Client struct {
	client *openai.Client
	model  string
}

// NewClient creates an LLM client pointing at the given base URL.
func NewClient(baseURL, apiKey, model string) *Client {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &Client{client: &client, model: model}
}

// Complete sends a system+user prompt and returns the assistant response.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", fmt.Errorf("LLM completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	return resp.Choices[0].Message.Content, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/llm/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/
git commit -m "feat: add LLM client wrapping openai-go SDK"
```

---

### Task 4: Extend Run model with triage and verifier fields

**Files:**
- Modify: `internal/models/run.go`

- [ ] **Step 1: Add new types and extend Run**

Add to `internal/models/run.go`:

```go
// LLMCall records a single LLM invocation during a run.
type LLMCall struct {
	Agent    string `json:"agent"`
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
	Duration string `json:"duration"`
}
```

Add fields to the existing `Run` struct (after `HumanDecision`):

```go
	TriageDecision string     `json:"triage_decision,omitempty"`
	TriageReason   string     `json:"triage_reason,omitempty"`
	VerifierPass   *bool      `json:"verifier_pass,omitempty"`
	VerifierSummary string    `json:"verifier_summary,omitempty"`
	LLMCalls       []LLMCall  `json:"llm_calls,omitempty"`
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/models/run.go
git commit -m "feat: extend Run model with triage, verifier, and LLM fields"
```

---

### Task 5: Command runner

**Files:**
- Rewrite: `internal/execution/command_runner.go`
- Create: `internal/execution/command_runner_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/execution/command_runner_test.go`:

```go
package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunSuccess(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "echo hello world")
	if log.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", log.ExitCode)
	}
	if !strings.Contains(log.Stdout, "hello world") {
		t.Errorf("stdout = %q, want 'hello world'", log.Stdout)
	}
}

func TestRunFailure(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "false")
	if log.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestRunTimeout(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 1,
	}

	log := runner.Run(context.Background(), "sleep 30")
	if log.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1 for timeout", log.ExitCode)
	}
	if !strings.Contains(log.Stderr, "timed out") {
		t.Errorf("stderr should mention timeout, got %q", log.Stderr)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "echo error >&2")
	if !strings.Contains(log.Stderr, "error") {
		t.Errorf("stderr = %q, want 'error'", log.Stderr)
	}
}

func TestWorktreeSetupCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	cmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"touch file.txt",
		"git add .",
		"git commit -m init",
	}
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %q failed: %v\n%s", c, err, out)
		}
	}

	runner := &CommandRunner{
		RepoPath:       repoDir,
		UseWorktree:    true,
		TimeoutSeconds: 10,
	}

	if err := runner.Setup(); err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	defer runner.Cleanup()

	if runner.WorkDir == repoDir {
		t.Error("WorkDir should differ from RepoPath when using worktree")
	}
	if _, err := os.Stat(runner.WorkDir); err != nil {
		t.Errorf("worktree dir should exist: %v", err)
	}

	log := runner.Run(context.Background(), "ls file.txt")
	if log.ExitCode != 0 {
		t.Errorf("file.txt should exist in worktree, exit=%d stderr=%q", log.ExitCode, log.Stderr)
	}

	if err := runner.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(runner.WorkDir); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after cleanup")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/execution/ -v
```

Expected: FAIL (types not defined).

- [ ] **Step 3: Write implementation**

Rewrite `internal/execution/command_runner.go`:

```go
package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// CommandRunner executes shell commands with timeout and output capture.
type CommandRunner struct {
	WorkDir        string
	RepoPath       string
	UseWorktree    bool
	TimeoutSeconds int
	worktreePath   string
}

// Setup prepares the execution environment. If UseWorktree is true,
// creates a git worktree from RepoPath.
func (r *CommandRunner) Setup() error {
	if !r.UseWorktree {
		r.WorkDir = r.RepoPath
		return nil
	}

	wtDir, err := os.MkdirTemp("", "conduit-experiment-wt-*")
	if err != nil {
		return fmt.Errorf("creating temp dir for worktree: %w", err)
	}

	// git worktree add requires a non-existing path, so remove the empty dir
	os.Remove(wtDir)

	cmd := exec.Command("git", "worktree", "add", "--detach", wtDir)
	cmd.Dir = r.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating worktree: %w\n%s", err, out)
	}

	r.worktreePath = wtDir
	r.WorkDir = wtDir
	return nil
}

// Cleanup removes the worktree if one was created.
func (r *CommandRunner) Cleanup() error {
	if r.worktreePath == "" {
		return nil
	}

	// Remove the worktree directory first
	os.RemoveAll(r.worktreePath)

	// Prune the worktree reference
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = r.RepoPath
	cmd.Run() // best-effort

	wtPath := r.worktreePath
	r.worktreePath = ""
	r.WorkDir = r.RepoPath

	_ = wtPath
	return nil
}

// Run executes a shell command and returns a CommandLog with captured output.
func (r *CommandRunner) Run(ctx context.Context, command string) models.CommandLog {
	timeout := time.Duration(r.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()

	log := models.CommandLog{
		Command: command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
		RunAt:   startTime,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.ExitCode = -1
			log.Stderr = log.Stderr + fmt.Sprintf("\ncommand timed out after %s", timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			log.ExitCode = exitErr.ExitCode()
		} else {
			log.ExitCode = -1
			log.Stderr = log.Stderr + fmt.Sprintf("\ncommand error: %v", err)
		}
	}

	// Resolve symlinks for WorkDir display
	resolvedDir := r.WorkDir
	if resolved, err := filepath.EvalSymlinks(r.WorkDir); err == nil {
		resolvedDir = resolved
	}
	_ = resolvedDir

	return log
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/execution/ -v
```

Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/execution/
git commit -m "feat: implement command runner with timeout and worktree support"
```

---

### Task 6: Triage agent

**Files:**
- Rewrite: `internal/agents/triage.go`
- Create: `internal/agents/triage_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agents/triage_test.go`:

```go
package agents

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
)

func TestTriageAccept(t *testing.T) {
	task := models.Task{
		ID:          "task-001",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{
		TaskID:       "task-001",
		RelatedFiles: []string{"README.md"},
	}
	policy := orchestrator.DefaultPhase1Policy()

	decision := Triage(task, dossier, policy)
	if decision.Decision != "accept" {
		t.Errorf("decision = %q, want accept", decision.Decision)
	}
}

func TestTriageRejectDifficulty(t *testing.T) {
	task := models.Task{
		ID:          "task-002",
		Difficulty:  models.DifficultyL4,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{TaskID: "task-002"}
	policy := orchestrator.DefaultPhase1Policy()

	decision := Triage(task, dossier, policy)
	if decision.Decision != "reject" {
		t.Errorf("decision = %q, want reject", decision.Decision)
	}
}

func TestTriageRejectBlastRadius(t *testing.T) {
	task := models.Task{
		ID:          "task-003",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusHigh,
	}
	dossier := models.Dossier{TaskID: "task-003"}
	policy := orchestrator.DefaultPhase1Policy()

	decision := Triage(task, dossier, policy)
	if decision.Decision != "reject" {
		t.Errorf("decision = %q, want reject", decision.Decision)
	}
}

func TestTriageDefer(t *testing.T) {
	task := models.Task{
		ID:          "task-004",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{
		TaskID:       "task-004",
		RelatedFiles: nil,
		OpenQuestions: []string{"What files are affected?"},
	}
	policy := orchestrator.DefaultPhase1Policy()

	decision := Triage(task, dossier, policy)
	if decision.Decision != "defer" {
		t.Errorf("decision = %q, want defer", decision.Decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agents/ -v -run TestTriage
```

Expected: FAIL (Triage not defined).

- [ ] **Step 3: Write implementation**

Rewrite `internal/agents/triage.go`:

```go
package agents

import (
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
)

// TriageDecision records the triage outcome for a task.
type TriageDecision struct {
	Decision string `json:"decision"` // "accept", "reject", "defer"
	Reason   string `json:"reason"`
}

// Triage evaluates whether a task should proceed based on policy and dossier quality.
func Triage(task models.Task, dossier models.Dossier, policy orchestrator.Policy) TriageDecision {
	if err := policy.CheckTask(task); err != nil {
		return TriageDecision{
			Decision: "reject",
			Reason:   err.Error(),
		}
	}

	if len(dossier.RelatedFiles) == 0 && len(dossier.RelatedDocs) == 0 && len(dossier.OpenQuestions) > 0 {
		return TriageDecision{
			Decision: "defer",
			Reason:   "no related files found and open questions remain",
		}
	}

	return TriageDecision{
		Decision: "accept",
		Reason:   "task within policy limits and dossier has relevant context",
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/agents/ -v -run TestTriage
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agents/triage.go internal/agents/triage_test.go
git commit -m "feat: implement triage agent with policy-based decisions"
```

---

### Task 7: Verifier agent

**Files:**
- Rewrite: `internal/agents/verifier.go`
- Create: `internal/agents/verifier_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agents/verifier_test.go`:

```go
package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestVerifyAllPass(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: []string{"echo test1", "true"},
	}

	report := Verify(context.Background(), runner, dossier)
	if !report.OverallPass {
		t.Error("expected OverallPass=true when all commands succeed")
	}
	if len(report.Commands) != 2 {
		t.Errorf("commands count = %d, want 2", len(report.Commands))
	}
}

func TestVerifyWithFailure(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: []string{"true", "false", "echo after"},
	}

	report := Verify(context.Background(), runner, dossier)
	if report.OverallPass {
		t.Error("expected OverallPass=false when a command fails")
	}
	if len(report.Commands) != 3 {
		t.Errorf("commands count = %d, want 3 (should run all commands)", len(report.Commands))
	}
}

func TestVerifyNoCommands(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: nil,
	}

	report := Verify(context.Background(), runner, dossier)
	if !report.OverallPass {
		t.Error("expected OverallPass=true when no commands to run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agents/ -v -run TestVerify
```

Expected: FAIL (Verify not defined).

- [ ] **Step 3: Write implementation**

Rewrite `internal/agents/verifier.go`:

```go
package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// VerifierReport summarizes the results of running validation commands.
type VerifierReport struct {
	Commands    []models.CommandLog `json:"commands"`
	OverallPass bool                `json:"overall_pass"`
	Summary     string              `json:"summary"`
}

// Verify runs each command from the dossier and collects results.
func Verify(ctx context.Context, runner *execution.CommandRunner, dossier models.Dossier) VerifierReport {
	if len(dossier.LikelyCommands) == 0 {
		return VerifierReport{
			OverallPass: true,
			Summary:     "no commands to run",
		}
	}

	var commands []models.CommandLog
	var failed []string
	for _, cmd := range dossier.LikelyCommands {
		log := runner.Run(ctx, cmd)
		commands = append(commands, log)
		if log.ExitCode != 0 {
			failed = append(failed, cmd)
		}
	}

	pass := len(failed) == 0
	total := len(dossier.LikelyCommands)
	passed := total - len(failed)

	var summary string
	if pass {
		summary = fmt.Sprintf("%d/%d commands passed", passed, total)
	} else {
		summary = fmt.Sprintf("%d/%d commands failed: %s", len(failed), total, strings.Join(failed, ", "))
	}

	return VerifierReport{
		Commands:    commands,
		OverallPass: pass,
		Summary:     summary,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/agents/ -v -run TestVerify
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agents/verifier.go internal/agents/verifier_test.go
git commit -m "feat: implement verifier agent with command execution"
```

---

### Task 8: Archivist agent

**Files:**
- Rewrite: `internal/agents/archivist.go`
- Create: `internal/agents/archivist_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agents/archivist_test.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func mockLLMServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gemini-2.5-flash",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": responseContent,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 50,
				"total_tokens":      60,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEnhanceDossier(t *testing.T) {
	llmResponse := `{
		"summary": "Update pipeline config docs to match current YAML syntax",
		"relevant_files": ["docs/pipeline-config.md", "internal/pipeline/config.go"],
		"relevant_docs": ["docs/design-documents/001-pipelines.md"],
		"suggested_commands": ["make test", "go build ./..."],
		"risks": ["Config examples may be referenced in external docs"],
		"open_questions": ["Are there other config files affected?"]
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config",
		Description: "Update pipeline configuration docs.",
		Labels:      []string{"docs", "pipeline"},
	}
	original := models.Dossier{
		TaskID:         "task-001",
		Summary:        "keyword-based summary",
		RelatedFiles:   []string{"file1.go", "file2.go", "file3.go"},
		RelatedDocs:    []string{"doc1.md"},
		LikelyCommands: []string{"go test ./..."},
		Risks:          []string{"none"},
		OpenQuestions:  []string{"original question"},
	}

	enhanced, llmCall, err := EnhanceDossier(context.Background(), client, task, original)
	if err != nil {
		t.Fatalf("EnhanceDossier() error: %v", err)
	}
	if enhanced.Summary != "Update pipeline config docs to match current YAML syntax" {
		t.Errorf("summary = %q, want LLM-enhanced summary", enhanced.Summary)
	}
	if len(enhanced.RelatedFiles) != 2 {
		t.Errorf("related files = %d, want 2", len(enhanced.RelatedFiles))
	}
	if llmCall.Agent != "archivist" {
		t.Errorf("llm call agent = %q, want archivist", llmCall.Agent)
	}
}

func TestEnhanceDossierFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"fail"}}`))
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{ID: "task-001", Title: "test"}
	original := models.Dossier{
		TaskID:  "task-001",
		Summary: "original summary",
	}

	enhanced, _, err := EnhanceDossier(context.Background(), client, task, original)
	if err != nil {
		t.Fatalf("expected fallback, not error: %v", err)
	}
	if enhanced.Summary != "original summary" {
		t.Errorf("should fall back to original dossier, got summary=%q", enhanced.Summary)
	}
}

func TestEnhanceDossierBadJSON(t *testing.T) {
	server := mockLLMServer(t, "this is not valid json at all")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{ID: "task-001", Title: "test"}
	original := models.Dossier{
		TaskID:  "task-001",
		Summary: "original summary",
	}

	enhanced, _, err := EnhanceDossier(context.Background(), client, task, original)
	if err != nil {
		t.Fatalf("expected fallback on bad JSON, not error: %v", err)
	}
	if enhanced.Summary != "original summary" {
		t.Error("should fall back to original dossier on bad JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agents/ -v -run TestEnhance
```

Expected: FAIL (EnhanceDossier not defined).

- [ ] **Step 3: Write implementation**

Rewrite `internal/agents/archivist.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const archivistSystemPrompt = `You are an expert software archivist. Given a maintenance task and a list of files from a repository, your job is to identify the most relevant files, docs, and commands for completing the task.

Respond with a JSON object containing exactly these fields:
- "summary": a concise 1-2 sentence summary of what the task requires
- "relevant_files": an array of the most relevant file paths (up to 20), ranked by relevance
- "relevant_docs": an array of the most relevant doc/ADR paths
- "suggested_commands": an array of commands to validate the work
- "risks": an array of potential risks
- "open_questions": an array of unresolved questions

Respond ONLY with the JSON object, no markdown fences or extra text.`

type archivistResponse struct {
	Summary          string   `json:"summary"`
	RelevantFiles    []string `json:"relevant_files"`
	RelevantDocs     []string `json:"relevant_docs"`
	SuggestedCommands []string `json:"suggested_commands"`
	Risks            []string `json:"risks"`
	OpenQuestions    []string `json:"open_questions"`
}

// EnhanceDossier uses an LLM to improve the keyword-based dossier.
// On LLM failure or bad response, it returns the original dossier unchanged.
// Returns the enhanced dossier, an LLMCall record for logging, and any error
// (nil on fallback — fallback is not an error).
func EnhanceDossier(ctx context.Context, client *llm.Client, task models.Task, original models.Dossier) (models.Dossier, models.LLMCall, error) {
	userPrompt := buildArchivistPrompt(task, original)

	start := time.Now()
	response, err := client.Complete(ctx, archivistSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "archivist",
		Model:    "gemini-2.5-flash",
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		log.Printf("archivist LLM call failed, using keyword dossier: %v", err)
		return original, call, nil
	}

	var parsed archivistResponse
	cleaned := strings.TrimSpace(response)
	// Strip markdown code fences if present
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		log.Printf("archivist response not valid JSON, using keyword dossier: %v", err)
		return original, call, nil
	}

	enhanced := models.Dossier{
		TaskID:         original.TaskID,
		Summary:        parsed.Summary,
		RelatedFiles:   parsed.RelevantFiles,
		RelatedDocs:    parsed.RelevantDocs,
		LikelyCommands: parsed.SuggestedCommands,
		Risks:          parsed.Risks,
		OpenQuestions:  parsed.OpenQuestions,
	}

	if enhanced.Summary == "" {
		enhanced.Summary = original.Summary
	}
	if len(enhanced.RelatedFiles) == 0 {
		enhanced.RelatedFiles = original.RelatedFiles
	}
	if len(enhanced.LikelyCommands) == 0 {
		enhanced.LikelyCommands = original.LikelyCommands
	}

	return enhanced, call, nil
}

func buildArchivistPrompt(task models.Task, dossier models.Dossier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n")
	fmt.Fprintf(&b, "ID: %s\n", task.ID)
	fmt.Fprintf(&b, "Title: %s\n", task.Title)
	fmt.Fprintf(&b, "Description: %s\n", task.Description)
	fmt.Fprintf(&b, "Difficulty: %s\n", task.Difficulty)
	fmt.Fprintf(&b, "Blast Radius: %s\n\n", task.BlastRadius)

	if len(task.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n\n", strings.Join(task.Labels, ", "))
	}

	fmt.Fprintf(&b, "## Candidate Files (%d total)\n", len(dossier.RelatedFiles))
	for _, f := range dossier.RelatedFiles {
		fmt.Fprintf(&b, "- %s\n", f)
	}

	fmt.Fprintf(&b, "\n## Candidate Docs (%d total)\n", len(dossier.RelatedDocs))
	for _, d := range dossier.RelatedDocs {
		fmt.Fprintf(&b, "- %s\n", d)
	}

	fmt.Fprintf(&b, "\n## Current Commands\n")
	for _, c := range dossier.LikelyCommands {
		fmt.Fprintf(&b, "- %s\n", c)
	}

	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/agents/ -v
```

Expected: PASS (all agent tests).

- [ ] **Step 5: Commit**

```bash
git add internal/agents/archivist.go internal/agents/archivist_test.go
git commit -m "feat: implement archivist agent with LLM-enhanced dossier"
```

---

### Task 9: Orchestrator workflow

**Files:**
- Rewrite: `internal/orchestrator/workflow.go`
- Create: `internal/orchestrator/workflow_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/orchestrator/workflow_test.go`:

```go
package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestRunWorkflowAccepted(t *testing.T) {
	// Set up fake repo
	repoDir := t.TempDir()
	files := map[string]string{
		"README.md":                              "# Test Project",
		"Makefile":                               "test:\n\techo ok",
		"docs/design-documents/001-init.md":      "# ADR 001",
		"internal/pipeline/pipeline.go":          "package pipeline",
	}
	for relPath, content := range files {
		full := filepath.Join(repoDir, relPath)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	// Set up mock LLM server
	llmResp := `{"summary":"Enhanced summary","relevant_files":["README.md"],"relevant_docs":["docs/design-documents/001-init.md"],"suggested_commands":["echo test"],"risks":["none"],"open_questions":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "test", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": llmResp},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Execution: config.ExecutionConfig{UseWorktree: false, TimeoutSeconds: 10},
		Reporting: config.ReportingConfig{OutputDir: t.TempDir()},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift",
		Description: "Update docs.",
		Labels:      []string{"docs"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
		Status:      models.TaskStatusPending,
	}

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != "accept" {
		t.Errorf("triage = %q, want accept", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusSuccess {
		t.Errorf("status = %q, want success", result.Run.FinalStatus)
	}
	if len(result.LLMCalls) == 0 {
		t.Error("expected at least one LLM call")
	}
}

func TestRunWorkflowRejected(t *testing.T) {
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644)

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir},
		Execution: config.ExecutionConfig{TimeoutSeconds: 10},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: "http://unused"},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-002",
		Title:       "Dangerous task",
		Difficulty:  models.DifficultyL4,
		BlastRadius: models.BlastRadiusHigh,
	}

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != "reject" {
		t.Errorf("triage = %q, want reject", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusRejected {
		t.Errorf("status = %q, want rejected", result.Run.FinalStatus)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/orchestrator/ -v -run TestRunWorkflow
```

Expected: FAIL (RunWorkflow not defined).

- [ ] **Step 3: Write implementation**

Rewrite `internal/orchestrator/workflow.go`:

```go
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/retrieval"
)

// WorkflowResult holds all artifacts produced by a single task run.
type WorkflowResult struct {
	Run            models.Run
	Dossier        models.Dossier
	Task           models.Task
	TriageDecision agents.TriageDecision
	VerifierReport agents.VerifierReport
	LLMCalls       []models.LLMCall
}

// RunWorkflow executes the full agent pipeline for a task.
func RunWorkflow(ctx context.Context, task models.Task, cfg config.Config, mcfg config.ModelsConfig) (*WorkflowResult, error) {
	startTime := time.Now()
	runID := fmt.Sprintf("run-%s-%s", task.ID, startTime.Format("20060102-150405"))
	agentsInvoked := []string{}

	// Stage 1: Ingest repo and build keyword dossier
	inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}
	dossier := retrieval.BuildDossier(task, inv)

	// Stage 2: Archivist — enhance dossier via LLM
	var llmCalls []models.LLMCall
	archModel := "gemini-2.5-flash"
	if rc, ok := mcfg.Roles["archivist"]; ok {
		archModel = rc.Model
	}
	llmClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, archModel)
	enhanced, llmCall, err := agents.EnhanceDossier(ctx, llmClient, task, dossier)
	if err != nil {
		return nil, fmt.Errorf("archivist: %w", err)
	}
	dossier = enhanced
	llmCalls = append(llmCalls, llmCall)
	agentsInvoked = append(agentsInvoked, "archivist")

	// Stage 3: Triage
	policy := DefaultPhase1Policy()
	triageDecision := agents.Triage(task, dossier, policy)
	agentsInvoked = append(agentsInvoked, "triage")

	run := models.Run{
		ID:             runID,
		TaskID:         task.ID,
		StartedAt:      startTime,
		AgentsInvoked:  agentsInvoked,
		TriageDecision: triageDecision.Decision,
		TriageReason:   triageDecision.Reason,
		LLMCalls:       llmCalls,
		HumanDecision:  models.HumanDecisionPending,
	}

	if triageDecision.Decision != "accept" {
		status := models.RunStatusRejected
		if triageDecision.Decision == "defer" {
			status = models.RunStatusFailed
		}
		run.FinalStatus = status
		run.EndedAt = time.Now()
		return &WorkflowResult{
			Run:            run,
			Dossier:        dossier,
			Task:           task,
			TriageDecision: triageDecision,
			LLMCalls:       llmCalls,
		}, nil
	}

	// Stage 4: Verifier — run commands
	runner := &execution.CommandRunner{
		RepoPath:       cfg.Target.RepoPath,
		UseWorktree:    cfg.Execution.UseWorktree,
		TimeoutSeconds: cfg.Execution.TimeoutSeconds,
	}
	if err := runner.Setup(); err != nil {
		return nil, fmt.Errorf("setting up command runner: %w", err)
	}
	defer runner.Cleanup()

	verifierReport := agents.Verify(ctx, runner, dossier)
	agentsInvoked = append(agentsInvoked, "verifier")

	pass := verifierReport.OverallPass
	run.AgentsInvoked = agentsInvoked
	run.CommandsRun = verifierReport.Commands
	run.VerifierPass = &pass
	run.VerifierSummary = verifierReport.Summary
	run.EndedAt = time.Now()

	if verifierReport.OverallPass {
		run.FinalStatus = models.RunStatusSuccess
	} else {
		run.FinalStatus = models.RunStatusFailed
	}

	return &WorkflowResult{
		Run:            run,
		Dossier:        dossier,
		Task:           task,
		TriageDecision: triageDecision,
		VerifierReport: verifierReport,
		LLMCalls:       llmCalls,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/orchestrator/ -v -run TestRunWorkflow
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/workflow.go internal/orchestrator/workflow_test.go
git commit -m "feat: implement orchestrator workflow chaining all agents"
```

---

### Task 10: Update markdown report with new sections

**Files:**
- Modify: `internal/reporting/markdown_report.go`
- Modify: `internal/reporting/markdown_report_test.go`

- [ ] **Step 1: Update the test**

Replace the contents of `TestRenderMarkdown` in `internal/reporting/markdown_report_test.go` to cover the new fields:

```go
func TestRenderMarkdown(t *testing.T) {
	pass := true
	run := models.Run{
		ID:              "run-001",
		TaskID:          "task-001",
		StartedAt:       time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
		EndedAt:         time.Date(2026, 4, 2, 12, 5, 0, 0, time.UTC),
		AgentsInvoked:   []string{"archivist", "triage", "verifier"},
		FinalStatus:     models.RunStatusSuccess,
		HumanDecision:   models.HumanDecisionPending,
		TriageDecision:  "accept",
		TriageReason:    "task within policy limits",
		VerifierPass:    &pass,
		VerifierSummary: "3/3 commands passed",
		CommandsRun: []models.CommandLog{
			{Command: "make test", ExitCode: 0, Stdout: "ok"},
			{Command: "go build ./...", ExitCode: 0, Stdout: ""},
		},
	}

	dossier := models.Dossier{
		TaskID:         "task-001",
		Summary:        "LLM-enhanced summary of the task",
		RelatedFiles:   []string{"docs/pipeline-config.md", "internal/pipeline/config.go"},
		RelatedDocs:    []string{"docs/design-documents/001-pipelines.md"},
		LikelyCommands: []string{"make test", "go build ./..."},
		Risks:          []string{"No major risks"},
		OpenQuestions:  []string{"Are all affected files identified?"},
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config example",
		Description: "Update docs to match current config behavior.",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}

	md, err := RenderMarkdown(run, dossier, task)
	if err != nil {
		t.Fatalf("RenderMarkdown() error: %v", err)
	}

	checks := []string{
		"# Run Report: run-001",
		"## Task",
		"task-001",
		"Fix docs drift in pipeline config example",
		"## Dossier",
		"LLM-enhanced summary",
		"docs/pipeline-config.md",
		"docs/design-documents/001-pipelines.md",
		"## Likely Commands",
		"make test",
		"## Risks",
		"## Open Questions",
		"## Triage",
		"accept",
		"task within policy limits",
		"## Verification",
		"3/3 commands passed",
		"make test",
		"## Run Details",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/reporting/ -v -run TestRenderMarkdown
```

Expected: FAIL (template doesn't have Triage/Verification sections yet).

- [ ] **Step 3: Update the template and reportData**

Replace the `reportTemplate` constant and `reportData` struct in `internal/reporting/markdown_report.go`:

```go
const reportTemplate = `# Run Report: {{ .Run.ID }}

## Task

| Field | Value |
|-------|-------|
| ID | {{ .Task.ID }} |
| Title | {{ .Task.Title }} |
| Difficulty | {{ .Task.Difficulty }} |
| Blast Radius | {{ .Task.BlastRadius }} |

{{ .Task.Description }}

## Dossier

**Summary:** {{ .Dossier.Summary }}

### Related Files
{{ range .Dossier.RelatedFiles }}
- {{ . }}
{{- end }}

### Related Docs
{{ range .Dossier.RelatedDocs }}
- {{ . }}
{{- end }}

## Likely Commands
{{ range .Dossier.LikelyCommands }}
- ` + "`{{ . }}`" + `
{{- end }}

## Risks
{{ range .Dossier.Risks }}
- {{ . }}
{{- end }}

## Open Questions
{{ range .Dossier.OpenQuestions }}
- {{ . }}
{{- end }}
{{ if .Run.TriageDecision }}
## Triage

| Field | Value |
|-------|-------|
| Decision | {{ .Run.TriageDecision }} |
| Reason | {{ .Run.TriageReason }} |
{{ end }}
{{ if .Run.VerifierSummary }}
## Verification

**Result:** {{ .Run.VerifierSummary }}
{{ if .Run.CommandsRun }}
| Command | Exit Code |
|---------|-----------|
{{ range .Run.CommandsRun -}}
| ` + "`{{ .Command }}`" + ` | {{ .ExitCode }} |
{{ end }}
{{- end }}
{{- end }}

## Run Details

| Field | Value |
|-------|-------|
| Started | {{ .Run.StartedAt.Format "2006-01-02 15:04:05 UTC" }} |
| Ended | {{ .Run.EndedAt.Format "2006-01-02 15:04:05 UTC" }} |
| Status | {{ .Run.FinalStatus }} |
| Human Decision | {{ .Run.HumanDecision }} |
| Agents | {{ joinStrings .Run.AgentsInvoked ", " }} |
`
```

Also update the `reportTmpl` var to use `strings.Join` (it should already from the simplify pass).

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/reporting/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reporting/
git commit -m "feat: add triage and verification sections to markdown report"
```

---

### Task 11: Wire CLI to orchestrator workflow

**Files:**
- Modify: `cmd/experiment/main.go`
- Modify: `configs/models.yaml`
- Modify: `.env.example`

- [ ] **Step 1: Update configs/models.yaml**

Replace `configs/models.yaml`:

```yaml
provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"

roles:
  archivist:
    model: "gemini-2.5-flash"
  triage:
    model: "gemini-2.5-flash"
  implementer:
    model: "gemini-2.5-flash"
  verifier:
    model: "gemini-2.5-flash"
  architect:
    model: "gemini-2.5-flash"
```

- [ ] **Step 2: Update .env.example**

Replace `.env.example`:

```
# Path to the local Conduit repository checkout
CONDUIT_REPO_PATH=/path/to/conduit

# LLM API key (Gemini via OpenAI-compatible endpoint)
GEMINI_API_KEY=

# Experiment settings
MAX_TASK_DIFFICULTY=L2
ALLOW_PUSH=false
ALLOW_MERGE=false
```

- [ ] **Step 3: Rewrite the run command in main.go**

Replace the `newRunCmd` function in `cmd/experiment/main.go`:

```go
func newRunCmd() *cobra.Command {
	var taskPath string
	var modelsFile string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a task against the target repository",
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

			task, err := loadTask(taskPath)
			if err != nil {
				return fmt.Errorf("loading task: %w", err)
			}

			fmt.Printf("Running task %s: %s\n", task.ID, task.Title)
			result, err := orchestrator.RunWorkflow(cmd.Context(), task, cfg, mcfg)
			if err != nil {
				return fmt.Errorf("workflow failed: %w", err)
			}

			fmt.Printf("Triage: %s (%s)\n", result.TriageDecision.Decision, result.TriageDecision.Reason)

			if result.TriageDecision.Decision != "accept" {
				fmt.Printf("Task %s, skipping verification\n", result.TriageDecision.Decision)
			} else {
				fmt.Printf("Verification: %s\n", result.VerifierReport.Summary)
			}

			outDir := filepath.Join(cfg.Reporting.OutputDir, result.Run.ID)
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			if err := reporting.WriteRunJSON(outDir, result.Run); err != nil {
				return fmt.Errorf("writing run JSON: %w", err)
			}
			if err := reporting.WriteDossierJSON(outDir, result.Dossier); err != nil {
				return fmt.Errorf("writing dossier JSON: %w", err)
			}

			md, err := reporting.RenderMarkdown(result.Run, result.Dossier, result.Task)
			if err != nil {
				return fmt.Errorf("rendering markdown: %w", err)
			}
			reportPath := filepath.Join(outDir, "report.md")
			if err := os.WriteFile(reportPath, []byte(md), 0644); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}

			fmt.Printf("\nRun complete: %s\n", result.Run.ID)
			fmt.Printf("Status: %s\n", result.Run.FinalStatus)
			fmt.Printf("Output: %s/\n", outDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&taskPath, "task", "", "path to task JSON file (required)")
	cmd.Flags().StringVar(&modelsFile, "models", "configs/models.yaml", "models config file path")
	cmd.MarkFlagRequired("task")
	return cmd
}
```

Update the imports in `main.go` — remove `"time"` (no longer used directly), remove `retrieval` import. The `orchestrator` import is already present. Ensure `config` is imported.

- [ ] **Step 4: Verify it compiles**

```bash
go build ./cmd/experiment/
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/experiment/main.go configs/models.yaml .env.example
git commit -m "feat: wire CLI run command to orchestrator workflow"
```

---

### Task 12: Update integration test

**Files:**
- Modify: `cmd/experiment/main_test.go`

- [ ] **Step 1: Rewrite the integration test**

Replace `cmd/experiment/main_test.go` to test through the orchestrator:

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/reporting"
)

func TestEndToEnd(t *testing.T) {
	repoDir := t.TempDir()
	repoFiles := map[string]string{
		"README.md":                              "# Conduit\nA streaming data platform.",
		"docs/pipeline-config.md":                "# Pipeline Config\nYAML-based pipeline configuration.",
		"docs/design-documents/001-pipelines.md": "# ADR: Pipeline Design",
		"internal/pipeline/pipeline.go":          "package pipeline",
		"Makefile":                               "test:\n\techo ok",
	}
	for relPath, content := range repoFiles {
		full := filepath.Join(repoDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	llmResp := `{"summary":"Enhanced task summary","relevant_files":["README.md"],"relevant_docs":["docs/design-documents/001-pipelines.md"],"suggested_commands":["echo test"],"risks":["none"],"open_questions":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "test", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": llmResp},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	outDir := t.TempDir()
	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Execution: config.ExecutionConfig{UseWorktree: false, TimeoutSeconds: 10},
		Reporting: config.ReportingConfig{OutputDir: outDir},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config example",
		Source:      "seeded",
		Description: "Update pipeline configuration docs.",
		Labels:      []string{"docs", "pipeline"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
		Status:      models.TaskStatusPending,
	}

	result, err := orchestrator.RunWorkflow(context.Background(), task, cfg, mcfg)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}

	if result.TriageDecision.Decision != "accept" {
		t.Fatalf("triage = %q, want accept", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusSuccess {
		t.Errorf("status = %q, want success", result.Run.FinalStatus)
	}

	runDir := filepath.Join(outDir, result.Run.ID)
	os.MkdirAll(runDir, 0755)

	if err := reporting.WriteRunJSON(runDir, result.Run); err != nil {
		t.Fatalf("WriteRunJSON error: %v", err)
	}
	if err := reporting.WriteDossierJSON(runDir, result.Dossier); err != nil {
		t.Fatalf("WriteDossierJSON error: %v", err)
	}
	md, err := reporting.RenderMarkdown(result.Run, result.Dossier, result.Task)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	os.WriteFile(filepath.Join(runDir, "report.md"), []byte(md), 0644)

	for _, name := range []string{"run.json", "dossier.json", "report.md"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	runData, _ := os.ReadFile(filepath.Join(runDir, "run.json"))
	var loadedRun models.Run
	if err := json.Unmarshal(runData, &loadedRun); err != nil {
		t.Fatalf("run.json invalid: %v", err)
	}
	if loadedRun.TriageDecision != "accept" {
		t.Errorf("run.json triage = %q, want accept", loadedRun.TriageDecision)
	}
}
```

- [ ] **Step 2: Run all tests**

```bash
go test ./... -v
```

Expected: PASS on all packages.

- [ ] **Step 3: Commit**

```bash
git add cmd/experiment/main_test.go
git commit -m "test: update integration test for milestone 1 workflow"
```

---

### Task 13: Smoke test with real Gemini API

Run the full CLI against the real Conduit repo with a real Gemini API key.

- [ ] **Step 1: Run the CLI**

```bash
CONDUIT_REPO_PATH=/Users/william-meroxa/Development/conduit GEMINI_API_KEY=$GEMINI_API_KEY go run ./cmd/experiment run --task data/tasks/task-001.json
```

Expected output showing:
- Indexed files count
- Triage: accept
- Verification results
- Run complete with output path

- [ ] **Step 2: Check the report**

```bash
cat data/runs/run-task-001-*/report.md
```

Expected: markdown with LLM-enhanced summary, triage section, verification section.

- [ ] **Step 3: Check the JSON**

```bash
cat data/runs/run-task-001-*/run.json | python3 -m json.tool | head -30
```

Expected: valid JSON with triage_decision, verifier_pass, llm_calls fields.

- [ ] **Step 4: Clean up and commit any fixes**

```bash
rm -rf data/runs/run-task-001-*
git add -A && git commit -m "fix: address issues found during smoke testing"
```

Only commit if fixes were needed.
