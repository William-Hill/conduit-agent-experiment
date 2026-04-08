# Per-Step Cost Tracking and Budget Controls

**Issue:** #21
**Date:** 2026-04-07

## Problem

The pipeline has no visibility into actual token usage or cost. Estimates are manual. There are no budget caps to prevent runaway spend.

## Design

## 1. Token Fields on LLMCall

Add `InputTokens` and `OutputTokens` to `models.LLMCall`:

```go
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

## 2. Token Extraction

**OpenAI-compatible path** (`internal/llm/client.go`):

Change `Complete()` to return `(string, int, int, error)` — the two extra ints are `inputTokens` and `outputTokens`, extracted from `resp.Usage.PromptTokens` and `resp.Usage.CompletionTokens`.

Update `callLLM()` in `internal/agents/util.go` to populate the new `LLMCall` fields from these return values.

**Anthropic path** (`internal/implementer/agent.go`):

Each `BetaMessage` in the tool runner loop has `.Usage.InputTokens` and `.Usage.OutputTokens`. Sum across all iterations. Add totals to `implementer.Result`:

```go
type Result struct {
    Summary      string
    Iterations   int
    InputTokens  int
    OutputTokens int
}
```

## 3. Cost Calculation

New package `internal/cost/` with `pricing.go`:

Hardcoded per-model pricing:

| Model | Input $/MTok | Output $/MTok |
|-------|-------------|---------------|
| gemini-2.5-flash | $0.15 | $0.60 |
| claude-haiku-4-5-20251001 | $1.00 | $5.00 |
| claude-sonnet-4-6-20250514 | $3.00 | $15.00 |

Functions:
- `Calculate(model string, inputTokens, outputTokens int) float64` — cost in USD for one call
- `CalculateCalls(calls []models.LLMCall) float64` — sum across a slice of calls

Unknown models use the most expensive known pricing as a safe fallback and log a warning.

## 4. Budget Controls

New file `internal/cost/budget.go`:

`Budget` struct reads caps from environment variables:
- `PIPELINE_MAX_COST` — total pipeline cap (default: no limit)
- `ARCHIVIST_MAX_COST` — archivist step cap
- `IMPL_MAX_COST` — implementer step cap
- `ARCHITECT_MAX_COST` — architect step cap

Methods:
- `CheckStep(step string, calls []models.LLMCall) error` — checks if a step exceeded its cap
- `CheckTotal(calls []models.LLMCall) error` — checks cumulative cost against pipeline cap

**Enforcement points:**
- `workflow.go`: after each LLM step, call `CheckStep()` then `CheckTotal()`. On error, set `run.FinalStatus = RunStatusFailed` with a descriptive reason and return early.
- `implementer/agent.go`: check running total against `IMPL_MAX_COST` after each tool runner iteration. Break out of the loop if exceeded.

## 5. Cost Report Artifact

New function `WriteCostReport(dir string, calls []models.LLMCall, budget Budget) error` in `internal/cost/`.

Writes `cost.json` alongside existing `run.json` and `evaluation.json`:

```json
{
  "total_cost_usd": 0.0042,
  "total_input_tokens": 12500,
  "total_output_tokens": 3200,
  "steps": [
    {
      "step": "archivist",
      "model": "gemini-2.5-flash",
      "input_tokens": 8000,
      "output_tokens": 1200,
      "cost_usd": 0.0019,
      "calls": 1
    }
  ],
  "budget": {
    "pipeline_cap_usd": 0.50,
    "exceeded": false
  }
}
```

Also populate the existing `Evaluation.LLMTokensUsed` field with total tokens.

## 6. CLI Output

Add a cost summary line after workflow completion in `cmd/experiment/main.go`:

```
Cost: $0.0042 (3 LLM calls, 15700 tokens) — budget: $0.50 remaining
```

## Files Changed

- `internal/models/run.go` — add token fields to `LLMCall`
- `internal/llm/client.go` — return token counts from `Complete()`
- `internal/llm/client_test.go` — update for new signature
- `internal/agents/util.go` — populate token fields in `callLLM()`
- `internal/implementer/agent.go` — sum tokens across iterations, add to `Result`
- `internal/cost/pricing.go` — new: pricing constants and `Calculate`/`CalculateCalls`
- `internal/cost/budget.go` — new: budget caps from env vars, check methods
- `internal/cost/report.go` — new: `WriteCostReport` for cost.json artifact
- `internal/orchestrator/workflow.go` — budget checks after each step
- `cmd/experiment/main.go` — write cost report artifact, cost summary in CLI output
- Tests for new `cost` package

## Acceptance Criteria Mapping

- [x] Token usage captured per pipeline step → sections 1-2
- [x] Cost computed using model pricing → section 3
- [x] Per-run cost report saved as JSON artifact → section 5
- [x] Budget cap halts pipeline when exceeded → section 4
- [x] Cost breakdown visible in CLI output → section 6
