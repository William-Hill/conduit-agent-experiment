package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llmutil"
	"google.golang.org/genai"
)

const reviewerSystemPrompt = `You are a code review engineer. You receive a GitHub issue, research context, and an implementation plan. Verify the plan is correct.

Check:
1. Does the plan address the issue?
2. Are the file paths real (they should match paths in the research context)?
3. Are the code changes syntactically correct Go?
4. Are the changes minimal and focused?

Output ONLY valid JSON:
{"approved": true, "feedback": "Plan looks good"}
or
{"approved": false, "feedback": "Issue: the plan modifies X but should modify Y..."}`

// ReviewPlan validates an implementation plan against the issue and dossier.
func ReviewPlan(ctx context.Context, geminiKey, issueTitle, issueBody string, dossier *archivist.Dossier, plan *ImplementationPlan) (*ReviewResult, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: geminiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("creating genai client: %w", err)
	}

	prompt := buildReviewerPrompt(issueTitle, issueBody, dossier, plan)

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(reviewerSystemPrompt, "user"),
		ResponseMIMEType:  "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("generating review: %w", err)
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

	var result ReviewResult
	if err := json.Unmarshal([]byte(llmutil.CleanJSON(text)), &result); err != nil {
		return nil, fmt.Errorf("parsing review JSON: %w", err)
	}

	return &result, nil
}

func buildReviewerPrompt(issueTitle, issueBody string, dossier *archivist.Dossier, plan *ImplementationPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Issue: %s\n\n%s\n\n", issueTitle, issueBody)
	fmt.Fprintf(&b, "## Research Summary\n\n%s\n\n", dossier.Summary)

	b.WriteString("## Relevant Files\n\n")
	for _, f := range dossier.Files {
		fmt.Fprintf(&b, "- %s: %s\n", f.Path, f.Reason)
	}

	fmt.Fprintf(&b, "\n## Implementation Plan\n\n%s\n", plan.Markdown)

	return b.String()
}
