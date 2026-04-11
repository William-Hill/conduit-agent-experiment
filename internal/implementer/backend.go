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
