package planner

import (
	"context"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"google.golang.org/genai"
)

const plannerSystemPrompt = `You are a senior Go engineer writing an implementation plan. You receive a GitHub issue and research context (relevant file contents).

Write a detailed markdown implementation document that a junior engineer can follow mechanically. For each file that needs to change:

1. State the file path
2. Show the EXACT code to write — complete functions, imports, or file sections
3. Use Go code blocks for all code
4. Explain what changed and why in one sentence

End with a "## Verification" section listing commands to run.

Be specific. Name functions, line numbers when relevant, exact import paths. The engineer should be able to copy-paste your code blocks.`

// CreatePlan calls Gemini to produce a markdown implementation plan.
func CreatePlan(ctx context.Context, geminiKey, issueTitle, issueBody string, dossier *archivist.Dossier) (*ImplementationPlan, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: geminiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("creating genai client: %w", err)
	}

	prompt := buildPlannerPrompt(issueTitle, issueBody, dossier)

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(plannerSystemPrompt, "user"),
		MaxOutputTokens:   32000,
	})
	if err != nil {
		return nil, fmt.Errorf("generating plan: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("empty response from model")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
	}

	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty plan text from model")
	}

	return &ImplementationPlan{Markdown: text}, nil
}

// cleanJSON strips markdown code fences from model output.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

func buildPlannerPrompt(issueTitle, issueBody string, dossier *archivist.Dossier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Issue: %s\n\n%s\n\n", issueTitle, issueBody)
	fmt.Fprintf(&b, "## Research Summary\n\n%s\n\n", dossier.Summary)
	fmt.Fprintf(&b, "## Suggested Approach\n\n%s\n\n", dossier.Approach)

	if len(dossier.Risks) > 0 {
		b.WriteString("## Risks\n\n")
		for _, r := range dossier.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Relevant Files\n\n")
	for _, f := range dossier.Files {
		fmt.Fprintf(&b, "### %s\n\nReason: %s\n\n```go\n%s\n```\n\n", f.Path, f.Reason, f.Content)
	}

	return b.String()
}
