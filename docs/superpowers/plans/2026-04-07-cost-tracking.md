# Per-Step Cost Tracking and Budget Controls

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track actual token usage and cost per pipeline step, enforce budget caps, and produce cost reports.

**Architecture:** Add token fields to `models.LLMCall`, extract usage from both OpenAI-compatible (Gemini) and Anthropic SDK responses, introduce an `internal/cost` package for pricing/budget/reporting, and wire budget checks into the orchestrator workflow.

**Tech Stack:** Go, `github.com/openai/openai-go` v1.12.0, `github.com/anthropics/anthropic-sdk-go` v1.30.0

---

### Task 1: Add token fields to `models.LLMCall`

**Files:**
- Modify: `internal/models/run.go:60-66`

- [ ] **Step 1: Add InputTokens and OutputTokens fields**

In `internal/models/run.go`, update the `LLMCall` struct:

```go
// LLMCall records a single LLM invocation during a run.
type LLMCall struct {
	Agent        string `json:"agent"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	Response     string `json:"response"`
	Duration     string `json:"duration"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS (new fields are zero-valued by default, so all existing code still compiles)

- [ ] **Step 3: Commit**

```bash
git add internal/models/run.go
git commit -m "feat(cost): add token count fields to LLMCall"
```

---

### Task 2: Return token counts from `llm.Client.Complete()`

**Files:**
- Modify: `internal/llm/client.go:27-44`
- Modify: `internal/llm/client_test.go`

- [ ] **Step 1: Update the test to expect token counts**

In `internal/llm/client_test.go`, update `TestComplete`:

```go
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
	result, inputTok, outputTok, err := client.Complete(context.Background(), "You are a helper.", "Say hello")
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result != "Hello from the LLM" {
		t.Errorf("result = %q, want 'Hello from the LLM'", result)
	}
	if inputTok != 10 {
		t.Errorf("inputTokens = %d, want 10", inputTok)
	}
	if outputTok != 5 {
		t.Errorf("outputTokens = %d, want 5", outputTok)
	}
}
```

Also update `TestCompleteError`:

```go
func TestCompleteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "server error"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gemini-2.5-flash")
	_, _, _, err := client.Complete(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -v`
Expected: FAIL — `Complete()` returns `(string, error)` not `(string, int, int, error)`

- [ ] **Step 3: Update `Complete()` to return token counts**

In `internal/llm/client.go`:

```go
// Complete sends a system+user prompt and returns the assistant response
// along with input and output token counts.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, int, int, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(c.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("LLM completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("LLM returned no choices")
	}

	return resp.Choices[0].Message.Content,
		int(resp.Usage.PromptTokens),
		int(resp.Usage.CompletionTokens),
		nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/llm/client.go internal/llm/client_test.go
git commit -m "feat(cost): return token counts from llm.Client.Complete()"
```

---

### Task 3: Populate token fields in `callLLM()` and fix callers

**Files:**
- Modify: `internal/agents/util.go:22-36`

- [ ] **Step 1: Update `callLLM()` to capture token counts**

In `internal/agents/util.go`:

```go
// callLLM makes an LLM completion call and records timing/metadata in an LLMCall.
func callLLM(ctx context.Context, client *llm.Client, agentName, modelName, systemPrompt, userPrompt string) (string, models.LLMCall, error) {
	start := time.Now()
	response, inputTokens, outputTokens, err := client.Complete(ctx, systemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:        agentName,
		Model:        modelName,
		Prompt:       userPrompt,
		Response:     response,
		Duration:     duration.String(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}

	return response, call, err
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS — `callLLM` callers (archivist, implementer, architect, selector) already destructure `(string, models.LLMCall, error)` and don't need changes.

- [ ] **Step 3: Run existing tests**

Run: `go test ./internal/agents/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agents/util.go
git commit -m "feat(cost): populate token counts in callLLM()"
```

---

### Task 4: Sum tokens in Anthropic implementer agent

**Files:**
- Modify: `internal/implementer/agent.go:28-29,72-89`
- Modify: `internal/implementer/agent_test.go`

- [ ] **Step 1: Add test for token fields on Result**

Append to `internal/implementer/agent_test.go`:

```go
func TestResultTokenFields(t *testing.T) {
	r := Result{
		Summary:      "wrote 2 files",
		Iterations:   3,
		InputTokens:  1500,
		OutputTokens: 800,
	}
	if r.InputTokens != 1500 {
		t.Errorf("InputTokens = %d, want 1500", r.InputTokens)
	}
	if r.OutputTokens != 800 {
		t.Errorf("OutputTokens = %d, want 800", r.OutputTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/implementer/ -v -run TestResultTokenFields`
Expected: FAIL — `Result` has no `InputTokens`/`OutputTokens` fields

- [ ] **Step 3: Add token fields to Result and sum in RunAgent**

In `internal/implementer/agent.go`, update the `Result` struct:

```go
// Result holds the outcome of an implementer agent run.
type Result struct {
	Summary      string
	Iterations   int
	InputTokens  int
	OutputTokens int
}
```

Update the `RunAgent` function — add accumulators before the loop and sum in the loop body:

```go
	var totalInput, totalOutput int64

	var finalMsg *anthropic.BetaMessage
	for msg, err := range runner.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("agent run failed at iteration %d: %w", runner.IterationCount(), err)
		}
		finalMsg = msg
		totalInput += msg.Usage.InputTokens
		totalOutput += msg.Usage.OutputTokens
		// Log tool calls for progress visibility
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				log.Printf("  [iter %d] tool: %s", runner.IterationCount(), block.Name)
			}
		}
	}

	return &Result{
		Summary:      extractText(finalMsg),
		Iterations:   runner.IterationCount(),
		InputTokens:  int(totalInput),
		OutputTokens: int(totalOutput),
	}, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/implementer/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/implementer/agent.go internal/implementer/agent_test.go
git commit -m "feat(cost): sum token usage across Anthropic implementer iterations"
```

---

### Task 5: Create `internal/cost/pricing.go` with cost calculation

**Files:**
- Create: `internal/cost/pricing.go`
- Create: `internal/cost/pricing_test.go`

- [ ] **Step 1: Write the tests**

Create `internal/cost/pricing_test.go`:

```go
package cost

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCalculateGeminiFlash(t *testing.T) {
	// 1000 input tokens at $0.15/MTok = $0.00015
	// 500 output tokens at $0.60/MTok = $0.00030
	got := Calculate("gemini-2.5-flash", 1000, 500)
	want := 0.00045
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Calculate(gemini-2.5-flash, 1000, 500) = %f, want %f", got, want)
	}
}

func TestCalculateHaiku(t *testing.T) {
	// 1000 input at $1.00/MTok = $0.001
	// 500 output at $5.00/MTok = $0.0025
	got := Calculate("claude-haiku-4-5-20251001", 1000, 500)
	want := 0.0035
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Calculate(haiku, 1000, 500) = %f, want %f", got, want)
	}
}

func TestCalculateUnknownModel(t *testing.T) {
	got := Calculate("unknown-model", 1000, 500)
	if got != 0.0 {
		t.Errorf("Calculate(unknown, ...) = %f, want 0.0", got)
	}
}

func TestCalculateCalls(t *testing.T) {
	calls := []models.LLMCall{
		{Model: "gemini-2.5-flash", InputTokens: 1000, OutputTokens: 500},
		{Model: "gemini-2.5-flash", InputTokens: 2000, OutputTokens: 1000},
	}
	got := CalculateCalls(calls)
	// Call 1: 0.00045, Call 2: 0.00090
	want := 0.00135
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CalculateCalls() = %f, want %f", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cost/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement pricing.go**

Create `internal/cost/pricing.go`:

```go
package cost

import (
	"log"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Price holds per-million-token pricing for a model.
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// modelPrices maps model identifiers to their pricing.
var modelPrices = map[string]Price{
	"gemini-2.5-flash":           {InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"claude-haiku-4-5-20251001":  {InputPerMTok: 1.00, OutputPerMTok: 5.00},
	"claude-sonnet-4-6-20250514": {InputPerMTok: 3.00, OutputPerMTok: 15.00},
}

// Calculate returns the cost in USD for the given token counts and model.
// Returns 0.0 for unknown models and logs a warning.
func Calculate(model string, inputTokens, outputTokens int) float64 {
	price, ok := modelPrices[model]
	if !ok {
		log.Printf("cost: unknown model %q, returning $0", model)
		return 0.0
	}
	inputCost := float64(inputTokens) / 1_000_000 * price.InputPerMTok
	outputCost := float64(outputTokens) / 1_000_000 * price.OutputPerMTok
	return inputCost + outputCost
}

// CalculateCalls sums the cost across a slice of LLM calls.
func CalculateCalls(calls []models.LLMCall) float64 {
	var total float64
	for _, c := range calls {
		total += Calculate(c.Model, c.InputTokens, c.OutputTokens)
	}
	return total
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cost/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cost/pricing.go internal/cost/pricing_test.go
git commit -m "feat(cost): add pricing constants and Calculate/CalculateCalls"
```

---

### Task 6: Create `internal/cost/budget.go` with budget controls

**Files:**
- Create: `internal/cost/budget.go`
- Create: `internal/cost/budget_test.go`

- [ ] **Step 1: Write the tests**

Create `internal/cost/budget_test.go`:

```go
package cost

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCheckStepUnderBudget(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{"archivist": 0.10}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 1000, OutputTokens: 500},
	}
	if err := b.CheckStep("archivist", calls); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckStepOverBudget(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{"archivist": 0.0001}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckStep("archivist", calls); err == nil {
		t.Error("expected budget exceeded error")
	}
}

func TestCheckStepNoCap(t *testing.T) {
	b := Budget{StepCaps: map[string]float64{}}
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckStep("archivist", calls); err != nil {
		t.Errorf("no cap should not error: %v", err)
	}
}

func TestCheckTotalOverBudget(t *testing.T) {
	b := Budget{PipelineCap: 0.001}
	calls := []models.LLMCall{
		{Model: "claude-haiku-4-5-20251001", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckTotal(calls); err == nil {
		t.Error("expected pipeline budget exceeded error")
	}
}

func TestCheckTotalNoCap(t *testing.T) {
	b := Budget{}
	calls := []models.LLMCall{
		{Model: "claude-haiku-4-5-20251001", InputTokens: 100000, OutputTokens: 50000},
	}
	if err := b.CheckTotal(calls); err != nil {
		t.Errorf("no cap should not error: %v", err)
	}
}

func TestLoadBudgetFromEnv(t *testing.T) {
	t.Setenv("PIPELINE_MAX_COST", "0.50")
	t.Setenv("ARCHIVIST_MAX_COST", "0.10")
	t.Setenv("IMPL_MAX_COST", "0.25")

	b := LoadBudget()
	if b.PipelineCap != 0.50 {
		t.Errorf("PipelineCap = %f, want 0.50", b.PipelineCap)
	}
	if b.StepCaps["archivist"] != 0.10 {
		t.Errorf("archivist cap = %f, want 0.10", b.StepCaps["archivist"])
	}
	if b.StepCaps["implementer"] != 0.25 {
		t.Errorf("implementer cap = %f, want 0.25", b.StepCaps["implementer"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cost/ -v -run TestCheck`
Expected: FAIL — `Budget` type does not exist

- [ ] **Step 3: Implement budget.go**

Create `internal/cost/budget.go`:

```go
package cost

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Budget holds cost caps for the pipeline and individual steps.
type Budget struct {
	PipelineCap float64            // 0 means no limit
	StepCaps    map[string]float64 // step name -> max cost in USD
}

// LoadBudget reads budget caps from environment variables.
func LoadBudget() Budget {
	b := Budget{
		StepCaps: make(map[string]float64),
	}

	b.PipelineCap = envFloat("PIPELINE_MAX_COST")

	if v := envFloat("ARCHIVIST_MAX_COST"); v > 0 {
		b.StepCaps["archivist"] = v
	}
	if v := envFloat("PLANNER_MAX_COST"); v > 0 {
		b.StepCaps["planner"] = v
	}
	if v := envFloat("IMPL_MAX_COST"); v > 0 {
		b.StepCaps["implementer"] = v
	}

	return b
}

// CheckStep checks whether the cost of calls for a specific step exceeds its cap.
func (b Budget) CheckStep(step string, calls []models.LLMCall) error {
	cap, ok := b.StepCaps[step]
	if !ok || cap <= 0 {
		return nil
	}

	var stepCost float64
	for _, c := range calls {
		if c.Agent == step || c.Agent == step+"-retry" {
			stepCost += Calculate(c.Model, c.InputTokens, c.OutputTokens)
		}
	}

	if stepCost > cap {
		return fmt.Errorf("budget exceeded for step %q: $%.4f > cap $%.4f", step, stepCost, cap)
	}
	return nil
}

// CheckTotal checks whether the total cost of all calls exceeds the pipeline cap.
func (b Budget) CheckTotal(calls []models.LLMCall) error {
	if b.PipelineCap <= 0 {
		return nil
	}

	total := CalculateCalls(calls)
	if total > b.PipelineCap {
		return fmt.Errorf("pipeline budget exceeded: $%.4f > cap $%.4f", total, b.PipelineCap)
	}
	return nil
}

func envFloat(key string) float64 {
	s := os.Getenv(key)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cost/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cost/budget.go internal/cost/budget_test.go
git commit -m "feat(cost): add budget controls with env var caps"
```

---

### Task 7: Create `internal/cost/report.go` for cost report artifact

**Files:**
- Create: `internal/cost/report.go`
- Create: `internal/cost/report_test.go`

- [ ] **Step 1: Write the test**

Create `internal/cost/report_test.go`:

```go
package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestWriteCostReport(t *testing.T) {
	dir := t.TempDir()
	calls := []models.LLMCall{
		{Agent: "archivist", Model: "gemini-2.5-flash", InputTokens: 8000, OutputTokens: 1200},
		{Agent: "implementer", Model: "gemini-2.5-flash", InputTokens: 4500, OutputTokens: 2000},
	}
	budget := Budget{PipelineCap: 0.50}

	if err := WriteCostReport(dir, calls, budget); err != nil {
		t.Fatalf("WriteCostReport error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cost.json"))
	if err != nil {
		t.Fatalf("reading cost.json: %v", err)
	}

	var report CostReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("parsing cost.json: %v", err)
	}

	if report.TotalCostUSD <= 0 {
		t.Error("expected positive total cost")
	}
	if report.TotalInputTokens != 12500 {
		t.Errorf("TotalInputTokens = %d, want 12500", report.TotalInputTokens)
	}
	if report.TotalOutputTokens != 3200 {
		t.Errorf("TotalOutputTokens = %d, want 3200", report.TotalOutputTokens)
	}
	if len(report.Steps) != 2 {
		t.Errorf("len(Steps) = %d, want 2", len(report.Steps))
	}
	if report.BudgetInfo.PipelineCapUSD != 0.50 {
		t.Errorf("PipelineCapUSD = %f, want 0.50", report.BudgetInfo.PipelineCapUSD)
	}
	if report.BudgetInfo.Exceeded {
		t.Error("budget should not be exceeded")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cost/ -v -run TestWriteCostReport`
Expected: FAIL — `WriteCostReport` does not exist

- [ ] **Step 3: Implement report.go**

Create `internal/cost/report.go`:

```go
package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// CostReport is the JSON structure written to cost.json.
type CostReport struct {
	TotalCostUSD      float64      `json:"total_cost_usd"`
	TotalInputTokens  int          `json:"total_input_tokens"`
	TotalOutputTokens int          `json:"total_output_tokens"`
	Steps             []StepCost   `json:"steps"`
	BudgetInfo        BudgetInfo   `json:"budget"`
}

// StepCost holds cost data for a single pipeline step.
type StepCost struct {
	Step         string  `json:"step"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Calls        int     `json:"calls"`
}

// BudgetInfo records the budget state at the end of a run.
type BudgetInfo struct {
	PipelineCapUSD float64 `json:"pipeline_cap_usd"`
	Exceeded       bool    `json:"exceeded"`
}

// WriteCostReport builds a cost report from LLM calls and writes it to dir/cost.json.
func WriteCostReport(dir string, calls []models.LLMCall, budget Budget) error {
	// Group calls by agent (step).
	type stepAccum struct {
		model        string
		inputTokens  int
		outputTokens int
		calls        int
	}
	byStep := make(map[string]*stepAccum)
	// Preserve insertion order.
	var stepOrder []string

	for _, c := range calls {
		// Normalize retry agents (e.g. "archivist-retry" -> "archivist").
		step := c.Agent
		if len(step) > 6 && step[len(step)-6:] == "-retry" {
			step = step[:len(step)-6]
		}

		acc, ok := byStep[step]
		if !ok {
			acc = &stepAccum{model: c.Model}
			byStep[step] = acc
			stepOrder = append(stepOrder, step)
		}
		acc.inputTokens += c.InputTokens
		acc.outputTokens += c.OutputTokens
		acc.calls++
	}

	var steps []StepCost
	var totalInput, totalOutput int
	for _, step := range stepOrder {
		acc := byStep[step]
		totalInput += acc.inputTokens
		totalOutput += acc.outputTokens
		steps = append(steps, StepCost{
			Step:         step,
			Model:        acc.model,
			InputTokens:  acc.inputTokens,
			OutputTokens: acc.outputTokens,
			CostUSD:      Calculate(acc.model, acc.inputTokens, acc.outputTokens),
			Calls:        acc.calls,
		})
	}

	totalCost := CalculateCalls(calls)
	exceeded := budget.PipelineCap > 0 && totalCost > budget.PipelineCap

	report := CostReport{
		TotalCostUSD:      totalCost,
		TotalInputTokens:  totalInput,
		TotalOutputTokens: totalOutput,
		Steps:             steps,
		BudgetInfo: BudgetInfo{
			PipelineCapUSD: budget.PipelineCap,
			Exceeded:       exceeded,
		},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling cost report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cost.json"), data, 0644); err != nil {
		return fmt.Errorf("writing cost.json: %w", err)
	}
	return nil
}

// TotalTokens returns the sum of input + output tokens across all calls.
func TotalTokens(calls []models.LLMCall) int {
	var total int
	for _, c := range calls {
		total += c.InputTokens + c.OutputTokens
	}
	return total
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cost/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cost/report.go internal/cost/report_test.go
git commit -m "feat(cost): add WriteCostReport for cost.json artifact"
```

---

### Task 8: Wire budget checks into `orchestrator/workflow.go`

**Files:**
- Modify: `internal/orchestrator/workflow.go`

- [ ] **Step 1: Add cost import and budget initialization**

Add `"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"` to imports.

After the triage early-return block (line 74) and before the repo walk (line 77), add:

```go
	budget := cost.LoadBudget()
```

- [ ] **Step 2: Add budget check after archivist step**

After `agentsInvoked = append(agentsInvoked, "archivist")` (line 93), add:

```go
	if err := budget.CheckStep("archivist", llmCalls); err != nil {
		run := models.Run{
			ID: runID, TaskID: task.ID, StartedAt: startTime,
			AgentsInvoked: agentsInvoked, TriageDecision: triageDecision.Decision,
			TriageReason: err.Error(), FinalStatus: models.RunStatusFailed,
			HumanDecision: models.HumanDecisionPending, LLMCalls: llmCalls,
			EndedAt: time.Now(),
		}
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, LLMCalls: llmCalls}, nil
	}
	if err := budget.CheckTotal(llmCalls); err != nil {
		run := models.Run{
			ID: runID, TaskID: task.ID, StartedAt: startTime,
			AgentsInvoked: agentsInvoked, TriageDecision: triageDecision.Decision,
			TriageReason: err.Error(), FinalStatus: models.RunStatusFailed,
			HumanDecision: models.HumanDecisionPending, LLMCalls: llmCalls,
			EndedAt: time.Now(),
		}
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, LLMCalls: llmCalls}, nil
	}
```

- [ ] **Step 3: Add budget check after implementer plan step**

After `llmCalls = append(llmCalls, planCall)` (line 145), add the same `CheckStep("implementer", ...)` and `CheckTotal(...)` pattern. On failure, set `run.FinalStatus = RunStatusFailed` with the budget error as `TriageReason` and return early.

- [ ] **Step 4: Add budget check after architect step**

After `llmCalls = append(llmCalls, archCalls...)` (line 293), add `CheckStep("architect", ...)` and `CheckTotal(...)` with early return on failure.

- [ ] **Step 5: Add `WorkflowResult.Budget` field and populate it**

Add `Budget cost.Budget` to `WorkflowResult` struct. Set `result.Budget = budget` in the final return.

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/workflow.go
git commit -m "feat(cost): wire budget checks into orchestrator workflow"
```

---

### Task 9: Write cost report and populate `LLMTokensUsed` in evaluation

**Files:**
- Modify: `cmd/experiment/main.go`

- [ ] **Step 1: Add cost report write after evaluation write**

Add `"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"` to imports.

After the `evaluation.WriteEvaluationJSON` call (around line 130), add:

```go
			if err := cost.WriteCostReport(outDir, result.LLMCalls, result.Budget); err != nil {
				return fmt.Errorf("writing cost report: %w", err)
			}
```

- [ ] **Step 2: Populate `LLMTokensUsed` in evaluation**

In `internal/orchestrator/workflow.go`, update both `BuildEvaluation` call sites to include `LLMTokensUsed`:

For the success/normal path (around line 331):

```go
		LLMCalls:            len(llmCalls),
		LLMTokensUsed:       cost.TotalTokens(llmCalls),
```

For the all-files-failed path (around line 255):

```go
		LLMCalls:            len(llmCalls),
		LLMTokensUsed:       cost.TotalTokens(llmCalls),
```

- [ ] **Step 3: Add CLI cost summary line**

In `cmd/experiment/main.go`, after the "PR:" line (around line 147), add:

```go
			totalCost := cost.CalculateCalls(result.LLMCalls)
			totalTokens := cost.TotalTokens(result.LLMCalls)
			costLine := fmt.Sprintf("Cost: $%.4f (%d LLM calls, %d tokens)", totalCost, len(result.LLMCalls), totalTokens)
			if result.Budget.PipelineCap > 0 {
				remaining := result.Budget.PipelineCap - totalCost
				costLine += fmt.Sprintf(" — budget: $%.2f remaining", remaining)
			}
			fmt.Println(costLine)
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/experiment/main.go internal/orchestrator/workflow.go
git commit -m "feat(cost): write cost.json artifact and CLI cost summary"
```

---

### Task 10: Run full test suite and verify

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS for all packages

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Verify cost.json structure manually**

Run: `go test ./internal/cost/ -v -run TestWriteCostReport`
Expected: PASS — confirms the cost.json has correct structure

- [ ] **Step 4: Final commit if any fixups needed**

```bash
git add -A
git commit -m "fix(cost): address test suite issues"
```
