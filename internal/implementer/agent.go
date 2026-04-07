package implementer

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
)

const systemPrompt = `You are an AI software engineer fixing a GitHub issue on a Go project.

The repository has been cloned to your working directory. A research archivist has already analyzed the issue and provided relevant files, a suggested approach, and identified risks. This context is included below the issue description.

## Workflow
1. Read the archivist's context carefully — it contains the relevant files and suggested approach.
2. Implement the fix based on the suggested approach.
3. Use write_file to make your changes.
4. Run "go build ./..." to verify the build passes.
5. Run "go vet ./..." to check for issues.
6. If there are relevant tests, run them.
7. If build or tests fail, read the errors, fix them, and retry.
8. When done, run "git diff" to review your changes.

## Rules
- Make minimal, focused changes. Do not refactor unrelated code.
- Follow existing code patterns and conventions.
- Always verify your changes compile before finishing.
- If tests fail because of your changes, fix them.
- When done, state what you changed and why.
- You may use read_file, list_dir, search_files if you need additional context beyond what the archivist provided.`

// Result holds the outcome of an implementer agent run.
type Result struct {
	Summary    string
	Iterations int
}

// RunAgent executes the implementer agent against a cloned repo.
func RunAgent(ctx context.Context, apiKey, repoDir string, dossier *archivist.Dossier, issueTitle, issueBody string, maxIterations int) (*Result, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tools, err := NewTools(repoDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	userPrompt := buildPrompt(issueTitle, issueBody, dossier)

	runner := client.Beta.Messages.NewToolRunner(tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_6,
			MaxTokens: 16384,
			System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
			Messages: []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(userPrompt)),
			},
		},
		MaxIterations: maxIterations,
	})

	finalMsg, err := runner.RunToCompletion(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent run failed: %w", err)
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
