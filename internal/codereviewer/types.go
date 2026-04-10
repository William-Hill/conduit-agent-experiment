// Package codereviewer runs a post-implementer, pre-push verification
// gate: re-runs `go build`/`go vet` against the working tree, then
// makes a single Gemini Flash call to catch semantic problems the
// compiler can't see (stubs, unfinished code, unrelated changes).
//
// Motivation: the implementer's internal build check is not reliable —
// hallucinated symbols and stubs have made it all the way to published
// PRs. This package is an external verification layer that does not
// trust the agent's self-report.
package codereviewer

// Verdict is the final outcome of a code review.
type Verdict struct {
	Approved bool `json:"approved"`
	// Category indicates which gate failed, if any.
	// One of: "build", "vet", "lint", "semantic", "".
	Category string `json:"category,omitempty"`
	// Summary is a human-readable one-liner for logs and dashboards.
	Summary string `json:"summary"`
	// Feedback is the structured message passed back to the implementer
	// on retry. Concatenation of build errors, vet errors, and LLM
	// semantic feedback as applicable.
	Feedback string `json:"feedback,omitempty"`

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

	// Cost telemetry for the semantic LLM call (zero if skipped).
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// CheckResult is the outcome of a single deterministic check.
type CheckResult struct {
	Passed   bool
	ExitCode int
	// Output is combined stdout+stderr, truncated to 16 KiB.
	Output string
}
