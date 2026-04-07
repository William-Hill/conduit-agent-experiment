package implementer

import (
	"context"
	"fmt"
	"log"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
)

const systemPrompt = `You are an AI software engineer. You receive a GitHub issue AND pre-researched context (relevant file contents, suggested approach). Your job is to WRITE CODE, not explore.

## STRICT Rules
- The archivist already explored the repo. The relevant files are in your prompt. DO NOT spend iterations reading more files.
- Start writing changes by iteration 3. If you haven't called write_file by iteration 3, you are wasting budget.
- After writing, run "go build ./..." to verify. Fix any errors. That's it.
- You have a HARD BUDGET of 15 tool calls total. Use them on write_file and run_command, not read_file.

## Workflow
1. Read the archivist context below (already in this message — no tool calls needed).
2. Call write_file for each file you need to change (1-3 calls).
3. Call run_command with "go build ./..." to verify (1 call).
4. If build fails, fix and retry (2-4 calls).
5. Call run_command with "git diff" to confirm your changes (1 call).
6. State what you changed and why.

## When to use read_file
ONLY if the archivist missed a file you need to see. This should be rare (0-2 calls max).`

// Result holds the outcome of an implementer agent run.
type Result struct {
	Summary    string
	Iterations int
}

// RunAgent executes the implementer agent against a cloned repo.
// Model can be overridden (e.g. "claude-haiku-4-5-20251001"); defaults to Haiku 4.5.
func RunAgent(ctx context.Context, apiKey, modelName, repoDir string, dossier *archivist.Dossier, issueTitle, issueBody string, maxIterations int) (*Result, error) {
	if modelName == "" {
		modelName = string(anthropic.ModelClaudeHaiku4_5)
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tools, err := NewTools(repoDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	userPrompt := buildPrompt(issueTitle, issueBody, dossier)

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

	var finalMsg *anthropic.BetaMessage
	for msg, err := range runner.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("agent run failed at iteration %d: %w", runner.IterationCount(), err)
		}
		finalMsg = msg
		// Log tool calls for progress visibility
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				log.Printf("  [iter %d] tool: %s", runner.IterationCount(), block.Name)
			}
		}
	}

	return &Result{
		Summary:    extractText(finalMsg),
		Iterations: runner.IterationCount(),
	}, nil
}

func buildPrompt(issueTitle, issueBody string, dossier *archivist.Dossier) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Fix this GitHub issue:\n\n## %s\n\n%s\n", issueTitle, issueBody)

	if dossier != nil {
		sb.WriteString("\n---\n\n## Archivist Research\n\n")
		fmt.Fprintf(&sb, "### Summary\n%s\n\n", dossier.Summary)
		fmt.Fprintf(&sb, "### Suggested Approach\n%s\n\n", dossier.Approach)

		if len(dossier.Risks) > 0 {
			sb.WriteString("### Risks\n")
			for _, r := range dossier.Risks {
				fmt.Fprintf(&sb, "- %s\n", r)
			}
			sb.WriteString("\n")
		}

		sb.WriteString("### Relevant Files\n\n")
		for _, f := range dossier.Files {
			fmt.Fprintf(&sb, "#### %s\n**Reason:** %s\n```\n%s\n```\n\n", f.Path, f.Reason, f.Content)
		}
	}

	return sb.String()
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
