package implementer

import (
	"context"
	"fmt"
	"log"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
)

// Compile-time assertion that AnthropicBackend satisfies the Backend interface.
var _ Backend = (*AnthropicBackend)(nil)

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

	// params.TargetFiles is consumed by AiderBackend only; the Anthropic
	// backend lets the model discover relevant files via its tools.

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
		ModelName:           b.modelName,
		Summary:             extractText(finalMsg),
		Iterations:          runner.IterationCount(),
		InputTokens:         int(totalInput),
		OutputTokens:        int(totalOutput),
		CacheCreationTokens: int(totalCacheCreate),
		CacheReadTokens:     int(totalCacheRead),
		BudgetExceeded:      budgetExceeded,
	}, nil
}
