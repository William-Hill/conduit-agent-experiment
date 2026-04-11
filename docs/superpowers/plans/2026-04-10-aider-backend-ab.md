# Aider Backend + A/B Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an Aider+OpenRouter backend to the implementer behind an `IMPL_BACKEND=aider` env var, plus the measurement infrastructure (hallucinated-symbol counter, A/B driver, analysis CLI) needed to execute the controlled experiment in issue #38.

**Architecture:** Refactor `implementer.RunAgent` into a `Backend` interface with two implementations: `AnthropicBackend` (wraps the current `anthropic-sdk-go` BetaToolRunner logic unchanged) and `AiderBackend` (shells out to the `aider` CLI against an OpenRouter free-tier model). `cmd/implementer/main.go` reads `IMPL_BACKEND` to select. The `run-summary.json` artifact gains a `backend` field so the A/B analyzer can partition runs. A new `cmd/ab-analyze` loads runs from both arms and computes cost, hallucinated-symbol, and pass-rate metrics.

**Tech Stack:** Go 1.25, existing `github.com/anthropics/anthropic-sdk-go`, existing `internal/cost` + `internal/ingest/symbol_extractor`, Aider CLI (pipx install), OpenRouter API (OpenAI-compatible).

**Out of scope:** Running the actual 18 experiment iterations. The plan builds the infrastructure; the experiment execution is a separate manual step driven by the script this plan creates.

**Working branch:** `feat/38-aider-backend` (worktree at `.worktrees/aider-backend/`)

**Related:** Issue #38, `docs/evaluations/2026-04-10-external-tools-research.md`

---

### Task 1: Register OpenRouter free-tier model pricing

**Files:**
- Modify: `internal/cost/pricing.go`
- Modify: `internal/cost/pricing_test.go`

The AiderBackend runs free-tier models via OpenRouter. Registering them in `modelPrices` with zero cost means:
(a) budget checks always pass for free models, and
(b) the cost field in `run-summary.json` is recorded as `$0.0000` instead of the fallback $3/$15 Sonnet pricing.

- [ ] **Step 1: Find the existing pricing test file**

Run: `ls internal/cost/`
Expected: See `pricing.go` and `pricing_test.go`. If `pricing_test.go` does not exist, create it with the minimal skeleton:
```go
package cost

import "testing"
```

- [ ] **Step 2: Write failing test for the new free-tier entries**

Append to `internal/cost/pricing_test.go`:
```go
func TestOpenRouterFreeTierPricing(t *testing.T) {
	freeModels := []string{
		"openrouter/qwen/qwen-2.5-coder-32b-instruct:free",
		"openrouter/deepseek/deepseek-r1:free",
		"openrouter/meta-llama/llama-3.3-70b-instruct:free",
	}
	for _, m := range freeModels {
		got := Calculate(m, 1_000_000, 1_000_000)
		if got != 0.0 {
			t.Errorf("Calculate(%q, 1M, 1M) = %f, want 0.0 (free tier)", m, got)
		}
	}
}
```

- [ ] **Step 3: Run test and verify it fails**

Run: `go test ./internal/cost/ -run TestOpenRouterFreeTierPricing -v`
Expected: FAIL — each free model falls back to Sonnet pricing, so `Calculate` returns a non-zero value.

- [ ] **Step 4: Add the entries to `modelPrices`**

In `internal/cost/pricing.go`, update the `modelPrices` map:
```go
var modelPrices = map[string]Price{
	"gemini-2.5-flash":           {InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"claude-haiku-4-5-20251001":  {InputPerMTok: 1.00, OutputPerMTok: 5.00},
	"claude-sonnet-4-6-20250514": {InputPerMTok: 3.00, OutputPerMTok: 15.00},
	// OpenRouter free tier — 20 req/min, 200 req/day per model.
	// Tracked as $0 so budget checks pass for free-tier experiments.
	"openrouter/qwen/qwen-2.5-coder-32b-instruct:free":    {InputPerMTok: 0, OutputPerMTok: 0},
	"openrouter/deepseek/deepseek-r1:free":                {InputPerMTok: 0, OutputPerMTok: 0},
	"openrouter/meta-llama/llama-3.3-70b-instruct:free":   {InputPerMTok: 0, OutputPerMTok: 0},
}
```

- [ ] **Step 5: Run test and verify it passes**

Run: `go test ./internal/cost/ -v`
Expected: PASS — including the new `TestOpenRouterFreeTierPricing`.

- [ ] **Step 6: Commit**

```bash
git add internal/cost/pricing.go internal/cost/pricing_test.go
git commit -m "feat(cost): register OpenRouter free-tier models at zero price"
```

---

### Task 2: Define Backend interface and RunParams

**Files:**
- Create: `internal/implementer/backend.go`

The `Backend` interface is a pure type declaration — no tests, no behavior change. It's the contract both backends will satisfy. Introducing it first means subsequent refactors can be done one backend at a time without touching the call site.

- [ ] **Step 1: Create `internal/implementer/backend.go`**

```go
package implementer

import (
	"context"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

// RunParams is the common input to any implementer backend.
// Fields are read-only; backends must not mutate the plan or slices.
type RunParams struct {
	RepoDir       string                      // cloned target repo
	Plan          *planner.ImplementationPlan // planner markdown spec
	TargetFiles   []string                    // paths (relative to RepoDir) the planner intends to edit
	MaxIterations int                         // hard cap on agent loop iterations
	MaxCost       float64                     // USD budget cap; 0 means unlimited
}

// Backend executes an implementation plan against a cloned repository and
// returns the run result. Implementations differ in how they drive the LLM
// (direct SDK loop, CLI shell-out, etc.) but must produce a *Result with
// comparable token, cost, and iteration fields so the A/B analyzer can
// partition runs fairly.
type Backend interface {
	// Name returns a stable identifier for this backend, e.g.
	// "anthropic:claude-haiku-4-5-20251001" or
	// "aider:openrouter/qwen/qwen-2.5-coder-32b-instruct:free".
	// Recorded in run-summary.json for A/B analysis.
	Name() string

	// Run executes the plan and returns the run result. The returned
	// *Result must have Iterations, InputTokens, OutputTokens, and
	// BudgetExceeded populated. Cache fields may be zero for backends
	// that don't support prompt caching.
	Run(ctx context.Context, params RunParams) (*Result, error)
}
```

- [ ] **Step 2: Verify build still passes**

Run: `go build ./...`
Expected: Success. The interface is unused — Go allows this.

- [ ] **Step 3: Commit**

```bash
git add internal/implementer/backend.go
git commit -m "feat(implementer): add Backend interface and RunParams"
```

---

### Task 3: Refactor current RunAgent into AnthropicBackend

**Files:**
- Create: `internal/implementer/anthropic_backend.go`
- Modify: `internal/implementer/agent.go`

This task moves the existing `RunAgent` body verbatim into `AnthropicBackend.Run`, then rewrites `RunAgent` as a thin wrapper that constructs an `AnthropicBackend` and delegates. Existing tests (`TestExtractTextNil`, `TestBuildPromptWithPlan`, `TestBuildPromptNilPlan`, `TestResultTokenFields`) must stay green because they test helper functions, not `RunAgent` directly.

- [ ] **Step 1: Create `internal/implementer/anthropic_backend.go` with the new type**

```go
package implementer

import (
	"context"
	"fmt"
	"log"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
)

// AnthropicBackend is the reference implementer backend — it drives the
// Anthropic SDK BetaToolRunner with five custom tools against a cloned
// repository. This is the baseline arm for issue #38's A/B experiment.
type AnthropicBackend struct {
	apiKey    string
	modelName string
}

// NewAnthropicBackend constructs a backend for the given Anthropic API key
// and model. If modelName is empty, defaults to Claude Haiku 4.5.
func NewAnthropicBackend(apiKey, modelName string) *AnthropicBackend {
	if modelName == "" {
		modelName = string(anthropic.ModelClaudeHaiku4_5)
	}
	return &AnthropicBackend{apiKey: apiKey, modelName: modelName}
}

// Name returns "anthropic:<model>" for run-summary partitioning.
func (b *AnthropicBackend) Name() string {
	return "anthropic:" + b.modelName
}

// Run executes the plan via the Anthropic BetaToolRunner. The body is moved
// verbatim from the original RunAgent function.
func (b *AnthropicBackend) Run(ctx context.Context, params RunParams) (*Result, error) {
	client := anthropic.NewClient(option.WithAPIKey(b.apiKey))

	tools, err := NewTools(params.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	userPrompt := buildPrompt(params.Plan)

	// Mark system prompt and user context as cacheable so they aren't
	// re-billed at full input price on every iteration. Cache hits cost
	// 10% of input price — significant savings over 20+ iterations.
	cache := anthropic.NewBetaCacheControlEphemeralParam()

	runner := client.Beta.Messages.NewToolRunner(tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(b.modelName),
			MaxTokens: 16384,
			System: []anthropic.BetaTextBlockParam{{
				Text:         systemPrompt,
				CacheControl: cache,
			}},
			Messages: []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.BetaContentBlockParamUnion{
					OfText: &anthropic.BetaTextBlockParam{
						Text:         userPrompt,
						CacheControl: cache,
					},
				}),
			},
		},
		MaxIterations: params.MaxIterations,
	})

	var totalInput, totalOutput, totalCacheCreate, totalCacheRead int64
	var budgetExceeded bool

	var finalMsg *anthropic.BetaMessage
	for msg, err := range runner.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("agent run failed at iteration %d: %w", runner.IterationCount(), err)
		}
		finalMsg = msg
		totalInput += msg.Usage.InputTokens
		totalOutput += msg.Usage.OutputTokens
		totalCacheCreate += msg.Usage.CacheCreationInputTokens
		totalCacheRead += msg.Usage.CacheReadInputTokens
		// Log tool calls for progress visibility
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				log.Printf("  [iter %d] tool: %s", runner.IterationCount(), block.Name)
			}
		}
		// Check budget after each iteration, including cache tokens.
		if params.MaxCost > 0 {
			spent := cost.CalculateWithCache(b.modelName, int(totalInput), int(totalCacheCreate), int(totalCacheRead), int(totalOutput))
			if spent > params.MaxCost {
				log.Printf("  implementer budget exceeded: $%.4f > cap $%.4f, stopping", spent, params.MaxCost)
				budgetExceeded = true
				break
			}
		}
	}

	return &Result{
		Summary:             extractText(finalMsg),
		Iterations:          runner.IterationCount(),
		InputTokens:         int(totalInput),
		OutputTokens:        int(totalOutput),
		CacheCreationTokens: int(totalCacheCreate),
		CacheReadTokens:     int(totalCacheRead),
		BudgetExceeded:      budgetExceeded,
	}, nil
}
```

- [ ] **Step 2: Rewrite `internal/implementer/agent.go` to delegate**

Replace the entire file contents with:
```go
package implementer

import (
	"context"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

const systemPrompt = `You are a code writer. You receive an implementation plan with exact file contents. Your ONLY job is to write the files and verify the build.

## Steps
1. For each file in the plan, call write_file with the exact content provided.
2. Run "go build ./..." to verify.
3. If the build fails, read the error, fix the file, and retry.
4. Run "git diff --stat" to confirm changes.
5. State what you wrote.

Do NOT explore the codebase. Do NOT read files unless a build fails. Just write the planned files and verify.`

// Result holds the outcome of an implementer agent run.
type Result struct {
	Summary             string
	Iterations          int
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	BudgetExceeded      bool
}

// RunAgent is a thin compatibility wrapper that constructs an AnthropicBackend
// and runs it. New callers should construct a Backend directly via
// NewAnthropicBackend or NewAiderBackend and call Run with RunParams.
//
// Deprecated: prefer Backend.Run with RunParams for new code.
func RunAgent(ctx context.Context, apiKey, modelName, repoDir string, plan *planner.ImplementationPlan, maxIterations int, maxCost float64) (*Result, error) {
	backend := NewAnthropicBackend(apiKey, modelName)
	return backend.Run(ctx, RunParams{
		RepoDir:       repoDir,
		Plan:          plan,
		MaxIterations: maxIterations,
		MaxCost:       maxCost,
	})
}

func buildPrompt(plan *planner.ImplementationPlan) string {
	if plan == nil {
		return ""
	}
	return plan.Markdown
}

// extractText pulls all text content from a BetaMessage.
func extractText(msg *anthropic.BetaMessage) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, block := range msg.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 3: Run tests to verify nothing broke**

Run: `go test ./internal/implementer/ -v`
Expected: PASS — `TestExtractTextNil`, `TestBuildPromptWithPlan`, `TestBuildPromptNilPlan`, `TestResultTokenFields`, and all `tools_test.go` tests.

- [ ] **Step 4: Run full build and test suite to catch any call-site regressions**

Run: `go build ./... && go test ./...`
Expected: All packages green. In particular, `cmd/implementer/main.go` still compiles because `RunAgent`'s signature is unchanged.

- [ ] **Step 5: Add a smoke test for AnthropicBackend.Name()**

Append to `internal/implementer/agent_test.go`:
```go
func TestAnthropicBackendName(t *testing.T) {
	b := NewAnthropicBackend("key", "claude-haiku-4-5-20251001")
	want := "anthropic:claude-haiku-4-5-20251001"
	if got := b.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAnthropicBackendDefaultModel(t *testing.T) {
	b := NewAnthropicBackend("key", "")
	if !strings.Contains(b.Name(), "claude-haiku") {
		t.Errorf("default model should be Haiku, got %q", b.Name())
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/implementer/ -v -run AnthropicBackend`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/implementer/agent.go internal/implementer/agent_test.go internal/implementer/anthropic_backend.go
git commit -m "refactor(implementer): extract AnthropicBackend from RunAgent"
```

---

### Task 4: Implement AiderBackend (TDD with fake aider binary)

**Files:**
- Create: `internal/implementer/aider_backend.go`
- Create: `internal/implementer/aider_backend_test.go`

AiderBackend shells out to the `aider` CLI. For tests we inject a path to a fake `aider` bash script that mimics aider's output format (including a parseable token report line), so tests never touch the real network or require aider to be installed.

The real aider invocation will look like:
```
aider --message-file <spec.md> --yes --auto-commits --no-pretty \
      --model openrouter/qwen/qwen-2.5-coder-32b-instruct:free \
      <target files...>
```

Aider prints token usage and cost near the end of its stdout in a line like:
```
Tokens: 12.3k sent, 1.5k received. Cost: $0.00 message, $0.00 session.
```

We parse this regex-style to populate `Result.InputTokens`/`OutputTokens`. Free-tier runs report `$0.00` which matches our `modelPrices` entry.

- [ ] **Step 1: Write failing test — basic happy path with fake aider**

Create `internal/implementer/aider_backend_test.go`:
```go
package implementer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

// writeFakeAider writes a bash script to tempDir that mimics aider's output
// format and returns its path. Skips on Windows.
func writeFakeAider(t *testing.T, tempDir, stdout string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake aider script is bash-only")
	}
	path := filepath.Join(tempDir, "aider")
	script := "#!/bin/bash\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aider: %v", err)
	}
	return path
}

func TestAiderBackendName(t *testing.T) {
	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", "aider")
	want := "aider:openrouter/qwen/qwen-2.5-coder-32b-instruct:free"
	if got := b.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAiderBackendRunHappyPath(t *testing.T) {
	tempDir := t.TempDir()
	fakeOut := `Added file1.go to the chat.
Applied edit to file1.go
Commit abc1234 feat: thing
Tokens: 12.3k sent, 1.5k received. Cost: $0.00 message, $0.00 session.
`
	fakeAider := writeFakeAider(t, tempDir, fakeOut)

	// Minimal fake repo — aider is faked so no real edits happen.
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "file1.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", fakeAider)
	plan := &planner.ImplementationPlan{Markdown: "Do the thing"}
	result, err := b.Run(context.Background(), RunParams{
		RepoDir:       repoDir,
		Plan:          plan,
		TargetFiles:   []string{"file1.go"},
		MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.InputTokens != 12300 {
		t.Errorf("InputTokens = %d, want 12300", result.InputTokens)
	}
	if result.OutputTokens != 1500 {
		t.Errorf("OutputTokens = %d, want 1500", result.OutputTokens)
	}
	if result.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1 (single aider invocation)", result.Iterations)
	}
	if !strings.Contains(result.Summary, "abc1234") {
		t.Errorf("Summary should include commit sha, got %q", result.Summary)
	}
}

func TestAiderBackendRunAiderNonzeroExit(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "aider")
	// Script prints an error and exits non-zero.
	script := "#!/bin/bash\necho 'aider: rate limit exceeded' >&2\nexit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aider: %v", err)
	}

	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", path)
	plan := &planner.ImplementationPlan{Markdown: "Do the thing"}
	_, err := b.Run(context.Background(), RunParams{
		RepoDir:       t.TempDir(),
		Plan:          plan,
		MaxIterations: 10,
	})
	if err == nil {
		t.Fatal("expected error from non-zero aider exit, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should include stderr, got %q", err.Error())
	}
}

func TestParseAiderTokenLine(t *testing.T) {
	cases := []struct {
		line     string
		wantIn   int
		wantOut  int
		wantOK   bool
	}{
		{"Tokens: 12.3k sent, 1.5k received. Cost: $0.00 message, $0.00 session.", 12300, 1500, true},
		{"Tokens: 500 sent, 250 received. Cost: $0.01 message, $0.02 session.", 500, 250, true},
		{"Tokens: 2.0M sent, 100k received. Cost: $0.00 message, $0.00 session.", 2_000_000, 100_000, true},
		{"no tokens line here", 0, 0, false},
	}
	for _, tc := range cases {
		in, out, ok := parseAiderTokens(tc.line)
		if ok != tc.wantOK || in != tc.wantIn || out != tc.wantOut {
			t.Errorf("parseAiderTokens(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tc.line, in, out, ok, tc.wantIn, tc.wantOut, tc.wantOK)
		}
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

Run: `go test ./internal/implementer/ -run Aider`
Expected: FAIL — `undefined: NewAiderBackend`, `undefined: parseAiderTokens`.

- [ ] **Step 3: Create `internal/implementer/aider_backend.go` with minimal implementation**

```go
package implementer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// AiderBackend drives the `aider` CLI (https://aider.chat) as the implementer.
// The spec is written to a temp file and passed via --message-file. Target
// files from the archivist's dossier are passed as positional args so aider
// scopes its edits to them. Cost/token parsing comes from aider's stdout
// "Tokens:" line.
type AiderBackend struct {
	openrouterKey string
	model         string // e.g. "openrouter/qwen/qwen-2.5-coder-32b-instruct:free"
	aiderPath     string // path to aider binary; "aider" for PATH lookup
}

// NewAiderBackend constructs a backend that shells out to the aider CLI.
// aiderPath of "" defaults to "aider" (resolved via PATH).
func NewAiderBackend(openrouterKey, model, aiderPath string) *AiderBackend {
	if aiderPath == "" {
		aiderPath = "aider"
	}
	if model == "" {
		model = "openrouter/qwen/qwen-2.5-coder-32b-instruct:free"
	}
	return &AiderBackend{
		openrouterKey: openrouterKey,
		model:         model,
		aiderPath:     aiderPath,
	}
}

// Name returns "aider:<model>" for run-summary partitioning.
func (b *AiderBackend) Name() string {
	return "aider:" + b.model
}

// Run writes the plan to a temp file, invokes aider non-interactively, and
// parses aider's stdout for token counts. Iterations is always 1 because
// aider runs as a single invocation (aider manages its own internal retries).
func (b *AiderBackend) Run(ctx context.Context, params RunParams) (*Result, error) {
	if params.Plan == nil {
		return nil, fmt.Errorf("aider backend: nil plan")
	}

	specFile, err := os.CreateTemp("", "aider-spec-*.md")
	if err != nil {
		return nil, fmt.Errorf("create spec file: %w", err)
	}
	defer os.Remove(specFile.Name())
	if _, err := specFile.WriteString(params.Plan.Markdown); err != nil {
		specFile.Close()
		return nil, fmt.Errorf("write spec file: %w", err)
	}
	specFile.Close()

	args := []string{
		"--message-file", specFile.Name(),
		"--yes",
		"--auto-commits",
		"--no-pretty",
		"--no-stream",
		"--model", b.model,
	}
	// Resolve target file paths against RepoDir so aider can find them.
	for _, f := range params.TargetFiles {
		args = append(args, filepath.Join(params.RepoDir, f))
	}

	cmd := exec.CommandContext(ctx, b.aiderPath, args...)
	cmd.Dir = params.RepoDir
	cmd.Env = append(os.Environ(),
		"OPENROUTER_API_KEY="+b.openrouterKey,
		"AIDER_ANALYTICS=false",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		return nil, fmt.Errorf("aider run failed: %w: %s", runErr, stderr.String())
	}

	out := stdout.String()
	inputTokens, outputTokens := 0, 0
	for _, line := range strings.Split(out, "\n") {
		if in, o, ok := parseAiderTokens(line); ok {
			inputTokens, outputTokens = in, o
			break
		}
	}

	return &Result{
		Summary:      extractAiderSummary(out),
		Iterations:   1,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// aiderTokenRe matches: "Tokens: 12.3k sent, 1.5k received. Cost: ..."
var aiderTokenRe = regexp.MustCompile(`Tokens:\s+([0-9.]+[kM]?)\s+sent,\s+([0-9.]+[kM]?)\s+received`)

// parseAiderTokens extracts (input, output) token counts from a single line
// of aider stdout. Returns ok=false if the line does not match.
func parseAiderTokens(line string) (int, int, bool) {
	m := aiderTokenRe.FindStringSubmatch(line)
	if len(m) != 3 {
		return 0, 0, false
	}
	in, ok1 := parseAiderNumber(m[1])
	out, ok2 := parseAiderNumber(m[2])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return in, out, true
}

// parseAiderNumber parses tokens like "12.3k", "1.5M", "500" into an int.
func parseAiderNumber(s string) (int, bool) {
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1_000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "M"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "M")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int(f * mult), true
}

// extractAiderSummary grabs the last commit line from aider stdout as a
// short run summary. Returns the last 2KB of output if no commit line found.
func extractAiderSummary(out string) string {
	var lastCommit string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Commit ") {
			lastCommit = strings.TrimSpace(line)
		}
	}
	if lastCommit != "" {
		return lastCommit
	}
	if len(out) > 2048 {
		return out[len(out)-2048:]
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/implementer/ -run Aider -v`
Expected: PASS — `TestAiderBackendName`, `TestAiderBackendRunHappyPath`, `TestAiderBackendRunAiderNonzeroExit`, `TestParseAiderTokenLine`.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: All packages green.

- [ ] **Step 6: Commit**

```bash
git add internal/implementer/aider_backend.go internal/implementer/aider_backend_test.go
git commit -m "feat(implementer): add Aider CLI backend with OpenRouter support"
```

---

### Task 5: Wire IMPL_BACKEND env var dispatch in cmd/implementer/main.go

**Files:**
- Modify: `cmd/implementer/main.go`

- [ ] **Step 1: Read the relevant section of main.go**

Run: `grep -n "implementer.RunAgent" cmd/implementer/main.go`
Expected: A single match near line 179.

- [ ] **Step 2: Replace the RunAgent call with backend dispatch**

Find this block in `cmd/implementer/main.go` (around lines 177–184):
```go
	// 8. Run implementer agent
	log.Printf("Running implementer agent (max %d iterations)...", maxIter)
	implMaxCost := cost.EnvFloat("IMPL_MAX_COST")
	result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxIter, implMaxCost)
	if err != nil {
		log.Fatalf("agent failed: %v", err)
	}
	log.Printf("Agent completed in %d iterations", result.Iterations)
	log.Printf("Summary: %s", result.Summary)
```

Replace with:
```go
	// 8. Run implementer agent
	backendName := os.Getenv("IMPL_BACKEND") // "", "anthropic", or "aider"
	implMaxCost := cost.EnvFloat("IMPL_MAX_COST")

	// Build target file list from the dossier so the Aider backend can scope edits.
	targetFiles := make([]string, 0, len(dossier.Files))
	for _, f := range dossier.Files {
		targetFiles = append(targetFiles, f.Path)
	}

	var backend implementer.Backend
	switch backendName {
	case "aider":
		openrouterKey := os.Getenv("OPENROUTER_API_KEY")
		if openrouterKey == "" {
			log.Fatalf("IMPL_BACKEND=aider requires OPENROUTER_API_KEY")
		}
		aiderModel := os.Getenv("IMPL_AIDER_MODEL") // may be empty → backend default
		backend = implementer.NewAiderBackend(openrouterKey, aiderModel, "")
	case "", "anthropic":
		backend = implementer.NewAnthropicBackend(anthropicKey, modelName)
	default:
		log.Fatalf("unknown IMPL_BACKEND=%q (want 'anthropic' or 'aider')", backendName)
	}

	log.Printf("Running implementer agent [%s] (max %d iterations)...", backend.Name(), maxIter)
	result, err := backend.Run(ctx, implementer.RunParams{
		RepoDir:       repoDir,
		Plan:          plan,
		TargetFiles:   targetFiles,
		MaxIterations: maxIter,
		MaxCost:       implMaxCost,
	})
	if err != nil {
		log.Fatalf("agent failed: %v", err)
	}
	log.Printf("Agent completed in %d iterations", result.Iterations)
	log.Printf("Summary: %s", result.Summary)
```

- [ ] **Step 3: Verify the full build**

Run: `go build ./...`
Expected: Success. If there is an unused import warning for the old `implementer` alias, Go will still build — check whether `implementer` is used elsewhere in main.go.

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: All green. `cmd/implementer` has no tests of its own; we're relying on the unit tests in `internal/implementer` plus the existing integration tests elsewhere.

- [ ] **Step 5: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): dispatch to backend via IMPL_BACKEND env var"
```

---

### Task 6: Record backend name in run-summary.json

**Files:**
- Modify: `cmd/implementer/main.go` (the `writeRunArtifacts` function)

The analyzer needs to partition runs by backend. Currently `run-summary.json` records `model` but not which backend drove it. Add a `backend` field populated from `backend.Name()`.

- [ ] **Step 1: Locate `writeRunArtifacts`**

Run: `grep -n "writeRunArtifacts" cmd/implementer/main.go`
Expected: Two matches — the call site and the function definition.

- [ ] **Step 2: Update the call site to pass the backend name**

Find the call in the main flow (it's the line near the earlier edit):
```go
		writeRunArtifacts(artifactDir, issue, result, modelName, plan)
```
Change to:
```go
		writeRunArtifacts(artifactDir, issue, result, backend.Name(), modelName, plan)
```

- [ ] **Step 3: Update the function signature and body**

Find `func writeRunArtifacts(` in the same file. Add a `backendName string` parameter and include it in the JSON map. Change:
```go
func writeRunArtifacts(dir string, issue *github.Issue, result *implementer.Result, modelName string, plan *planner.ImplementationPlan) {
```
To:
```go
func writeRunArtifacts(dir string, issue *github.Issue, result *implementer.Result, backendName, modelName string, plan *planner.ImplementationPlan) {
```

Inside the function, find the map literal that builds the summary JSON (it contains keys like `"model"`, `"iterations"`, `"input_tokens"`). Add a `"backend"` key:
```go
	summary := map[string]any{
		"backend":                backendName, // NEW
		"issue_number":           issue.Number,
		"issue_title":            issue.Title,
		"model":                  modelName,
		// ... rest unchanged ...
	}
```

- [ ] **Step 4: Verify build and test**

Run: `go build ./... && go test ./...`
Expected: All green.

- [ ] **Step 5: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): record backend name in run-summary.json"
```

---

### Task 7: Hallucinated symbol counter

**Files:**
- Create: `internal/implementer/hallucination.go`
- Create: `internal/implementer/hallucination_test.go`

Given a unified diff and a `*ingest.SymbolIndex` built from the target repo, count references to Go identifiers in the added lines that do NOT exist in the repo. This is the primary quality metric for the A/B.

Scope: we only need to catch symbols that look like Go identifiers (`[A-Z][A-Za-z0-9_]*` for exported, `[a-z][A-Za-z0-9_]*` for unexported) that appear in added lines of the diff. We filter out Go keywords and stdlib package prefixes. We then check each against `ingest.SearchSymbols`.

False positives from stdlib (`fmt`, `http`, `context`, etc.) are accepted — they'll inflate both arms equally, so the *delta* is what matters.

- [ ] **Step 1: Write failing test**

Create `internal/implementer/hallucination_test.go`:
```go
package implementer

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

// fakeIndex builds an in-memory SymbolIndex for testing.
func fakeIndex(symbols ...string) *ingest.SymbolIndex {
	idx := &ingest.SymbolIndex{}
	for _, name := range symbols {
		idx.Symbols = append(idx.Symbols, ingest.Symbol{Name: name, Kind: "func"})
	}
	return idx
}

func TestCountHallucinatedSymbols_NoHallucinations(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	Existing()
+}
`
	idx := fakeIndex("Bar", "Existing")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("got %d hallucinations, want 0", got)
	}
}

func TestCountHallucinatedSymbols_OneHallucination(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	DoesNotExist()
+}
`
	idx := fakeIndex("Bar")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 1 {
		t.Errorf("got %d hallucinations, want 1", got)
	}
}

func TestCountHallucinatedSymbols_IgnoresStdlib(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	fmt.Println("hi")
+	http.StatusOK
+}
`
	idx := fakeIndex("Bar")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("stdlib refs should not count as hallucinations, got %d", got)
	}
}

func TestCountHallucinatedSymbols_IgnoresKeywords(t *testing.T) {
	diff := `
+++ b/foo.go
+if true {
+	return nil
+}
`
	got := CountHallucinatedSymbols(diff, fakeIndex())
	if got != 0 {
		t.Errorf("keywords should not count, got %d", got)
	}
}

func TestCountHallucinatedSymbols_OnlyAddedLines(t *testing.T) {
	// Removed lines (starting with -) should be ignored.
	diff := `
+++ b/foo.go
-DoesNotExist()
+Existing()
`
	idx := fakeIndex("Existing")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("removed lines should be ignored, got %d", got)
	}
}
```

- [ ] **Step 2: Verify test fails with undefined error**

Run: `go test ./internal/implementer/ -run Hallucin`
Expected: FAIL — `undefined: CountHallucinatedSymbols`.

- [ ] **Step 3: Create `internal/implementer/hallucination.go`**

```go
package implementer

import (
	"regexp"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

// goKeywords lists Go keywords and common builtin identifiers. They're
// filtered out before symbol lookup.
var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
	"true": true, "false": true, "nil": true, "iota": true,
	"string": true, "int": true, "int32": true, "int64": true, "uint": true,
	"uint32": true, "uint64": true, "float32": true, "float64": true, "bool": true,
	"byte": true, "rune": true, "error": true, "any": true,
	"make": true, "new": true, "len": true, "cap": true, "append": true,
	"copy": true, "delete": true, "panic": true, "recover": true, "print": true,
	"println": true, "close": true,
}

// stdlibPackages is a non-exhaustive list of Go stdlib package selectors
// that appear at the start of qualified identifiers. Matches are treated
// as real references rather than hallucinations.
var stdlibPackages = map[string]bool{
	"fmt": true, "os": true, "io": true, "context": true, "errors": true,
	"http": true, "json": true, "time": true, "strings": true, "strconv": true,
	"bytes": true, "log": true, "sync": true, "sort": true, "regexp": true,
	"path": true, "filepath": true, "exec": true, "testing": true, "reflect": true,
	"bufio": true, "unicode": true, "math": true, "url": true,
}

// identRe matches Go identifiers — letters, digits, underscores, starting with
// a letter or underscore.
var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// CountHallucinatedSymbols counts identifiers in added diff lines that do
// NOT exist in the given symbol index. Stdlib package selectors, Go keywords,
// builtins, and identifiers shorter than 3 characters are ignored.
//
// The metric is approximate — the goal is A/B comparison, not absolute
// correctness. Both arms are scored the same way.
func CountHallucinatedSymbols(diff string, idx *ingest.SymbolIndex) int {
	if idx == nil {
		return 0
	}
	seen := make(map[string]bool)
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		body := line[1:]
		for _, ident := range identRe.FindAllString(body, -1) {
			if len(ident) < 3 || goKeywords[ident] || stdlibPackages[ident] {
				continue
			}
			if seen[ident] {
				continue
			}
			seen[ident] = true
			if len(ingest.SearchSymbols(idx, ident)) == 0 {
				count++
			}
		}
	}
	return count
}
```

- [ ] **Step 4: Verify symbol_extractor API — check SymbolIndex.Symbols field name and SearchSymbols signature**

Run: `grep -n "type SymbolIndex\|func SearchSymbols" internal/ingest/symbol_extractor.go`
Expected: See the struct definition and the function signature. Adjust the test file and `hallucination.go` if the actual field is named differently (e.g. `Symbols` vs `All` vs unexported). If the Symbols field is unexported, use a constructor function from the ingest package instead of literal struct construction in the test.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/implementer/ -run Hallucin -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/implementer/hallucination.go internal/implementer/hallucination_test.go
git commit -m "feat(implementer): add hallucinated symbol counter for A/B metric"
```

---

### Task 8: A/B experiment driver script

**Files:**
- Create: `scripts/ab-experiment.sh`

Shell script that runs the same seeded task N times under both backends and copies each run-summary.json into a flat `data/ab-runs/<arm>/<task>/run-<N>/` layout for the analyzer.

- [ ] **Step 1: Create `scripts/ab-experiment.sh`**

```bash
#!/bin/bash
# ab-experiment.sh — Run the same seeded tasks N times under both implementer
# backends (anthropic baseline vs aider+openrouter). Collects run-summary.json
# files into data/ab-runs/<arm>/<task>/run-<iter>/ for later analysis.
#
# Usage:
#   ./scripts/ab-experiment.sh <iterations_per_arm>
#
# Required env vars:
#   ANTHROPIC_API_KEY      — for arm A (anthropic)
#   OPENROUTER_API_KEY     — for arm B (aider)
#   GH_TOKEN               — for gh CLI used by the implementer
#   IMPL_REPO_OWNER        — target repo owner
#   IMPL_REPO_NAME         — target repo name
#   IMPL_FORK_OWNER        — your fork owner
#
# Optional:
#   IMPL_AIDER_MODEL       — override default Qwen3 Coder free model ID

set -euo pipefail

ITERS="${1:-3}"
TASKS=(576 645)  # gh-576 HTTP status codes, gh-645 version constant
BASE_DIR="data/ab-runs"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "$BASE_DIR"

run_one() {
  local backend="$1"
  local issue="$2"
  local iter="$3"
  local out_dir="$BASE_DIR/$backend/task-gh-$issue/run-$iter-$TIMESTAMP"

  mkdir -p "$out_dir"
  echo "=== $backend / task-gh-$issue / iter $iter ==="

  IMPL_BACKEND="$backend" \
  IMPL_ISSUE_NUMBER="$issue" \
  IMPL_ARTIFACT_DIR="$out_dir" \
    go run ./cmd/implementer 2>&1 | tee "$out_dir/stdout.log" || {
      echo "run failed — continuing"
      echo "{\"error\":\"run failed\"}" > "$out_dir/run-summary.json"
    }
}

for task in "${TASKS[@]}"; do
  for iter in $(seq 1 "$ITERS"); do
    run_one "anthropic" "$task" "$iter"
    run_one "aider" "$task" "$iter"
  done
done

echo
echo "All runs complete. Analyze with: go run ./cmd/ab-analyze $BASE_DIR"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/ab-experiment.sh`

- [ ] **Step 3: Smoke-test syntax**

Run: `bash -n scripts/ab-experiment.sh`
Expected: No output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add scripts/ab-experiment.sh
git commit -m "feat(experiments): add A/B driver script for implementer backends"
```

---

### Task 9: A/B analysis CLI

**Files:**
- Create: `cmd/ab-analyze/main.go`
- Create: `cmd/ab-analyze/main_test.go`

Loads all `run-summary.json` files under a given root, partitions by `backend` field, and prints a comparison table: N runs, mean cost, mean iterations, mean input tokens, mean output tokens, success rate (`budget_exceeded` = false AND no error key).

The hallucinated-symbol count is *not* computed by this analyzer — it requires the repo clone which is cleaned up after each run. Instead, Task 10 extends the implementer to record the hallucination count into run-summary.json at write time.

- [ ] **Step 1: Write failing test**

Create `cmd/ab-analyze/main_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSummary(t *testing.T, dir, backend string, cost float64, iterations int, budgetExceeded bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"backend":"` + backend + `","estimated_cost_usd":` + ftoa(cost) + `,"iterations":` + itoa(iterations) + `,"budget_exceeded":` + btoa(budgetExceeded) + `,"input_tokens":100,"output_tokens":50,"hallucinated_symbols":0}`
	if err := os.WriteFile(filepath.Join(dir, "run-summary.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzePartitionsByBackend(t *testing.T) {
	root := t.TempDir()
	writeSummary(t, filepath.Join(root, "a", "r1"), "anthropic:claude-haiku-4-5", 0.05, 5, false)
	writeSummary(t, filepath.Join(root, "a", "r2"), "anthropic:claude-haiku-4-5", 0.06, 6, false)
	writeSummary(t, filepath.Join(root, "b", "r1"), "aider:openrouter/qwen", 0.00, 1, false)
	writeSummary(t, filepath.Join(root, "b", "r2"), "aider:openrouter/qwen", 0.00, 1, true)

	report, err := analyze(root)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(report.Arms) != 2 {
		t.Errorf("got %d arms, want 2", len(report.Arms))
	}
	anthropic := report.Arm("anthropic:claude-haiku-4-5")
	if anthropic == nil || anthropic.Runs != 2 {
		t.Errorf("anthropic arm should have 2 runs, got %v", anthropic)
	}
	if anthropic.MeanCost < 0.054 || anthropic.MeanCost > 0.056 {
		t.Errorf("mean cost = %f, want ~0.055", anthropic.MeanCost)
	}
	aider := report.Arm("aider:openrouter/qwen")
	if aider == nil || aider.Runs != 2 {
		t.Errorf("aider arm should have 2 runs, got %v", aider)
	}
	if aider.SuccessRate != 0.5 {
		t.Errorf("aider success rate = %f, want 0.5", aider.SuccessRate)
	}
}

// small helpers to avoid importing strconv in the test file
func ftoa(f float64) string {
	return stdFormatFloat(f)
}
func itoa(i int) string { return stdFormatInt(i) }
func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
```

Then create `cmd/ab-analyze/main_helpers_test.go` (keeps test utilities separate from real code path):
```go
package main

import "strconv"

func stdFormatFloat(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }
func stdFormatInt(i int) string       { return strconv.Itoa(i) }
```

- [ ] **Step 2: Verify test fails (no main.go)**

Run: `go test ./cmd/ab-analyze/ 2>&1`
Expected: FAIL — missing `main` package file, `undefined: analyze`, etc.

- [ ] **Step 3: Create `cmd/ab-analyze/main.go`**

```go
// Command ab-analyze reads run-summary.json files from a directory tree and
// prints a comparison report partitioned by the `backend` field. Used by
// issue #38's implementer A/B experiment.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
)

type runSummary struct {
	Backend             string  `json:"backend"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	Iterations          int     `json:"iterations"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	BudgetExceeded      bool    `json:"budget_exceeded"`
	HallucinatedSymbols int     `json:"hallucinated_symbols"`
	Error               string  `json:"error"`
}

// ArmStats aggregates metrics across all runs for a given backend.
type ArmStats struct {
	Backend              string
	Runs                 int
	MeanCost             float64
	MeanIterations       float64
	MeanInputTokens      float64
	MeanOutputTokens     float64
	MeanHallucinated     float64
	SuccessRate          float64
}

// Report is the analyzed result across all arms.
type Report struct {
	Arms []*ArmStats
}

// Arm returns the arm with the given backend name or nil if not found.
func (r *Report) Arm(backend string) *ArmStats {
	for _, a := range r.Arms {
		if a.Backend == backend {
			return a
		}
	}
	return nil
}

func analyze(root string) (*Report, error) {
	byBackend := map[string][]runSummary{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "run-summary.json" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var rs runSummary
		if err := json.Unmarshal(body, &rs); err != nil {
			log.Printf("skip %s: %v", path, err)
			return nil
		}
		if rs.Error != "" {
			rs.BudgetExceeded = true // treat errors as failures for success rate
		}
		byBackend[rs.Backend] = append(byBackend[rs.Backend], rs)
		return nil
	})
	if err != nil {
		return nil, err
	}

	report := &Report{}
	for backend, runs := range byBackend {
		stats := &ArmStats{Backend: backend, Runs: len(runs)}
		var successes int
		for _, r := range runs {
			stats.MeanCost += r.EstimatedCostUSD
			stats.MeanIterations += float64(r.Iterations)
			stats.MeanInputTokens += float64(r.InputTokens)
			stats.MeanOutputTokens += float64(r.OutputTokens)
			stats.MeanHallucinated += float64(r.HallucinatedSymbols)
			if !r.BudgetExceeded && r.Error == "" {
				successes++
			}
		}
		n := float64(stats.Runs)
		stats.MeanCost /= n
		stats.MeanIterations /= n
		stats.MeanInputTokens /= n
		stats.MeanOutputTokens /= n
		stats.MeanHallucinated /= n
		stats.SuccessRate = float64(successes) / n
		report.Arms = append(report.Arms, stats)
	}
	sort.Slice(report.Arms, func(i, j int) bool { return report.Arms[i].Backend < report.Arms[j].Backend })
	return report, nil
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ab-analyze <runs-root>")
		os.Exit(2)
	}
	report, err := analyze(flag.Arg(0))
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}
	fmt.Printf("%-60s  %6s  %10s  %6s  %8s  %8s  %6s  %10s\n",
		"BACKEND", "RUNS", "SUCCESS%", "ITERS", "IN TOK", "OUT TOK", "HALLU", "MEAN COST")
	for _, a := range report.Arms {
		fmt.Printf("%-60s  %6d  %9.1f%%  %6.1f  %8.0f  %8.0f  %6.1f  $%9.4f\n",
			a.Backend, a.Runs, a.SuccessRate*100,
			a.MeanIterations, a.MeanInputTokens, a.MeanOutputTokens,
			a.MeanHallucinated, a.MeanCost)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ab-analyze/ -v`
Expected: PASS — `TestAnalyzePartitionsByBackend`.

- [ ] **Step 5: Run full build**

Run: `go build ./...`
Expected: Success.

- [ ] **Step 6: Commit**

```bash
git add cmd/ab-analyze/main.go cmd/ab-analyze/main_test.go cmd/ab-analyze/main_helpers_test.go
git commit -m "feat(experiments): add ab-analyze CLI for implementer A/B"
```

---

### Task 10: Record hallucinated symbol count in run-summary.json

**Files:**
- Modify: `cmd/implementer/main.go` (writeRunArtifacts or near the artifact write)

After the implementer runs, we have the cloned repoDir available until the final cleanup. Before cleanup, run `git diff` in repoDir, build a symbol index via `ingest.BuildSymbolIndex(repoDir)`, call `implementer.CountHallucinatedSymbols(diff, idx)`, and include the count in the artifact.

- [ ] **Step 1: Locate where `writeRunArtifacts` is called and where cleanup happens**

Run: `grep -n "writeRunArtifacts\|RemoveAll(repoDir)" cmd/implementer/main.go`
Expected: One call to writeRunArtifacts and one or more RemoveAll(repoDir) calls.

- [ ] **Step 2: Compute the metric before calling writeRunArtifacts**

Find the block where artifacts are written (around where Task 6 edited). Before the call to `writeRunArtifacts`, add:

```go
	// Metric for A/B (issue #38): count identifiers in the post-run diff
	// that do not appear in the target repo's symbol index. Purely additive —
	// if anything fails we record 0 rather than blocking the pipeline.
	var hallucinated int
	diffBytes, diffErr := exec.CommandContext(ctx, "git", "diff", "HEAD").Output()
	if diffErr == nil {
		if idx, ixErr := ingest.BuildSymbolIndex(repoDir); ixErr == nil {
			hallucinated = implementer.CountHallucinatedSymbols(string(diffBytes), idx)
			log.Printf("Hallucinated symbols in diff: %d", hallucinated)
		} else {
			log.Printf("symbol index build failed (continuing): %v", ixErr)
		}
	} else {
		log.Printf("git diff for hallucination metric failed (continuing): %v", diffErr)
	}
```

Then pass `hallucinated` into `writeRunArtifacts`:
```go
		writeRunArtifacts(artifactDir, issue, result, backend.Name(), modelName, plan, hallucinated)
```

- [ ] **Step 3: Update writeRunArtifacts signature and body**

In the function definition, add the parameter and the map entry:
```go
func writeRunArtifacts(dir string, issue *github.Issue, result *implementer.Result, backendName, modelName string, plan *planner.ImplementationPlan, hallucinatedSymbols int) {
	// ...
	summary := map[string]any{
		"backend":                backendName,
		"hallucinated_symbols":   hallucinatedSymbols, // NEW
		"issue_number":           issue.Number,
		// ... rest unchanged ...
	}
```

- [ ] **Step 4: Verify imports**

If the file does not already import `github.com/mjhilldigital/conduit-agent-experiment/internal/ingest`, add it. `os/exec` is already imported per existing usage of `exec.CommandContext`.

- [ ] **Step 5: Build and test**

Run: `go build ./... && go test ./...`
Expected: All green.

- [ ] **Step 6: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): record hallucinated symbol count in run-summary"
```

---

### Task 11: Update onboarding.md with Aider + OpenRouter setup

**Files:**
- Modify: `docs/onboarding.md`

- [ ] **Step 1: Read the current onboarding.md to find the right section**

Run: `grep -n "^##" docs/onboarding.md`
Expected: A list of top-level section headers. Find the section on environment variables or provider setup.

- [ ] **Step 2: Add a new section near the bottom, before any "troubleshooting" section**

Append (or insert at the appropriate location):
```markdown
## Optional: Aider + OpenRouter backend (experimental, issue #38)

The implementer supports a second backend that shells out to the
[Aider](https://aider.chat/) CLI and routes through
[OpenRouter](https://openrouter.ai/) — typically against a free-tier model
such as Qwen3 Coder. This is the experimental arm for the A/B prototype
tracked in issue #38; it is not yet the default.

### Install Aider

```bash
# Preferred: pipx (isolated, no global package pollution)
brew install pipx   # or: python3 -m pip install --user pipx
pipx install aider-chat

# Verify
aider --version
```

### Create an OpenRouter account and key

1. Sign up at https://openrouter.ai/
2. Create an API key under https://openrouter.ai/keys
3. Export it alongside your other API keys:

```bash
export OPENROUTER_API_KEY="sk-or-v1-…"
```

### Run the pipeline against the Aider backend

```bash
IMPL_BACKEND=aider \
IMPL_AIDER_MODEL="openrouter/qwen/qwen-2.5-coder-32b-instruct:free" \
OPENROUTER_API_KEY="sk-or-v1-…" \
  go run ./cmd/implementer
```

`IMPL_AIDER_MODEL` is optional; the backend defaults to a free-tier Qwen
Coder model. Other useful free-tier models:
- `openrouter/deepseek/deepseek-r1:free`
- `openrouter/meta-llama/llama-3.3-70b-instruct:free`

### Run the A/B experiment

```bash
./scripts/ab-experiment.sh 3     # 3 iterations per task per backend
go run ./cmd/ab-analyze data/ab-runs
```

### Rate limits

OpenRouter's free tier is capped at **20 requests/minute, 200 requests/day
per model**. The AiderBackend does not retry or round-robin across models —
if you hit the cap, wait or switch `IMPL_AIDER_MODEL` to a different free
model for the next run.
```

- [ ] **Step 3: Verify markdown renders (lint)**

Run: `markdownlint docs/onboarding.md 2>/dev/null || true`
Expected: No output, or only pre-existing warnings. (markdownlint may not be installed — skip if command not found.)

- [ ] **Step 4: Commit**

```bash
git add docs/onboarding.md
git commit -m "docs(onboarding): add Aider + OpenRouter backend setup"
```

---

### Task 12: Stub the "Prototype #1 results" section in the research doc

**Files:**
- Modify: `docs/evaluations/2026-04-10-external-tools-research.md`

- [ ] **Step 1: Append the results section placeholder**

At the bottom of `docs/evaluations/2026-04-10-external-tools-research.md`, before the Sources section, add:
```markdown
## Prototype #1 results (pending — tracked in #38)

**Status:** Infrastructure landed. Experiment runs not yet executed.

Once the runs are complete, fill in:

- Cost per arm (mean ± stdev)
- Success rate (builds, tests, internal reviewer pass)
- Mean hallucinated symbol count per arm
- Mean wall-clock time per run
- OpenRouter rate-limit events observed
- Qualitative notes: edit-format issues, spec compatibility, surprising behaviors

Run the experiment with:

```bash
./scripts/ab-experiment.sh 3
go run ./cmd/ab-analyze data/ab-runs > docs/evaluations/ab-results-raw.txt
```

Then paste the analyzer table here and write the recommendation (adopt /
reject / hybrid).
```

- [ ] **Step 2: Commit**

```bash
git add docs/evaluations/2026-04-10-external-tools-research.md
git commit -m "docs(evaluations): stub prototype #1 results section"
```

---

### Task 13: Final verification checkpoint

- [ ] **Step 1: Run the full build and test suite**

Run: `go build ./... && go test ./...`
Expected: All green.

- [ ] **Step 2: Verify the Backend interface can be constructed both ways without env vars**

Quick sanity-only check — no new test files needed. Just confirm by eye that:
- `NewAnthropicBackend("", "")` would use Haiku default
- `NewAiderBackend("", "", "")` would use Qwen Coder default

- [ ] **Step 3: Write commit summary**

Run: `git log --oneline main..HEAD`
Expected: ~12 commits, one per preceding task.

- [ ] **Step 4: Push branch and open draft PR against main**

```bash
git push -u origin feat/38-aider-backend
gh pr create --draft --title "feat: Aider backend + A/B harness (issue #38)" --body "Implements infrastructure for #38 A/B experiment. Does NOT run the 18 experiment iterations — those are a separate manual step with \`./scripts/ab-experiment.sh 3\`."
```

Expected: PR URL printed. Note that the draft is intentional — the experiment itself still has to run before we can close #38.

---

## Self-review checklist (for plan author — not execution steps)

Spec coverage vs issue #38 deliverables:

- [x] "New binary or flag on cmd/implementer to select implementer backend" — Tasks 2, 3, 4, 5
- [x] "Aider install + OpenRouter key provisioning documented in docs/onboarding.md" — Task 11
- [ ] "18 experiment runs (9 per arm) with results committed to data/runs/" — **intentionally out of scope**, gated by Task 8 script
- [x] "Analysis written up in docs/evaluations/..." — Task 12 stub + Task 9 analyzer
- [ ] "Recommendation: adopt, reject, or iterate" — follows experiment execution, not this plan

Cross-task type consistency:
- `Backend` interface defined in Task 2, implemented in Tasks 3 and 4, consumed in Task 5 ✓
- `Result` struct unchanged across refactor (Task 3) ✓
- `run-summary.json` fields `backend` (Task 6) + `hallucinated_symbols` (Task 10) consumed by `runSummary` in Task 9 ✓
- `RunParams` fields match the consumer in Task 5 ✓
- `NewAiderBackend(openrouterKey, model, aiderPath string)` signature identical in Tasks 4 and 5 ✓

Known soft spots (resolved during execution, not blockers):
1. **Task 7, Step 4** reserves explicit verification of the `ingest.SymbolIndex` API — the test file uses `idx.Symbols = append(...)` which may need adjustment if the field is unexported. Fix inline during execution.
2. **Aider's exact stdout format** for the token line was not verified against a real run. If the regex in Task 4 does not match real output, update `aiderTokenRe` during execution — the test uses a fake script, so regression is obvious when real aider is invoked.
3. **Task 5** deletes a `const` usage of `implementer.RunAgent` — if any other call site exists outside `cmd/implementer/main.go`, the compatibility wrapper in Task 3 Step 2 keeps it working.
