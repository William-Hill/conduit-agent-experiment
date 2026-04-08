package implementer

import (
	"context"
	"fmt"
	"log"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
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
	Summary        string
	Iterations     int
	InputTokens    int
	OutputTokens   int
	BudgetExceeded bool
}

// RunAgent executes the implementer agent against a cloned repo.
// Model can be overridden (e.g. "claude-haiku-4-5-20251001"); defaults to Haiku 4.5.
// maxCost is the budget cap in USD for this step (0 means no limit).
func RunAgent(ctx context.Context, apiKey, modelName, repoDir string, plan *planner.ImplementationPlan, maxIterations int, maxCost float64) (*Result, error) {
	if modelName == "" {
		modelName = string(anthropic.ModelClaudeHaiku4_5)
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tools, err := NewTools(repoDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	userPrompt := buildPrompt(plan)

	// Mark system prompt and user context as cacheable so they aren't
	// re-billed at full input price on every iteration. Cache hits cost
	// 10% of input price — significant savings over 20+ iterations.
	cache := anthropic.NewBetaCacheControlEphemeralParam()

	runner := client.Beta.Messages.NewToolRunner(tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(modelName),
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
		MaxIterations: maxIterations,
	})

	var totalInput, totalOutput int64
	var budgetExceeded bool

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
		// Check budget after each iteration.
		if maxCost > 0 {
			spent := cost.Calculate(modelName, int(totalInput), int(totalOutput))
			if spent > maxCost {
				log.Printf("  implementer budget exceeded: $%.4f > cap $%.4f, stopping", spent, maxCost)
				budgetExceeded = true
				break
			}
		}
	}

	return &Result{
		Summary:        extractText(finalMsg),
		Iterations:     runner.IterationCount(),
		InputTokens:    int(totalInput),
		OutputTokens:   int(totalOutput),
		BudgetExceeded: budgetExceeded,
	}, nil
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
