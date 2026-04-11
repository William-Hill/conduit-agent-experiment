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
