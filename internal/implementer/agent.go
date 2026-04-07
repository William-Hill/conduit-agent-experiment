package implementer

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const systemPrompt = `You are an AI software engineer fixing a GitHub issue on a Go project.

The repository has been cloned to your working directory. Use the provided tools to explore the codebase, implement the fix, and verify it compiles.

## Workflow
1. Read the issue description carefully.
2. Use list_dir and search_files to understand the relevant code.
3. Read the files you need to modify.
4. Write your changes using write_file.
5. Run "go build ./..." to verify the build passes.
6. Run "go vet ./..." to check for issues.
7. If there are relevant tests, run them with "go test ./path/to/package -v".
8. If build or tests fail, read the errors, fix them, and retry.
9. When done, run "git diff" to review your changes.

## Rules
- Make minimal, focused changes. Do not refactor unrelated code.
- Follow existing code patterns and conventions.
- If you create new files, follow the package structure of nearby files.
- Always verify your changes compile before finishing.
- If tests fail because of your changes, fix them.
- When done, state what you changed and why.`

// Result holds the outcome of an implementer agent run.
type Result struct {
	Summary    string
	Iterations int
}

// RunAgent executes the implementer agent against a cloned repo.
func RunAgent(ctx context.Context, apiKey, repoDir, issueTitle, issueBody string, maxIterations int) (*Result, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	tools, err := NewTools(repoDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	userPrompt := fmt.Sprintf("Fix this GitHub issue:\n\n## %s\n\n%s", issueTitle, issueBody)

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
