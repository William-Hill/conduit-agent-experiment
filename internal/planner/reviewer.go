package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"google.golang.org/genai"
)

const reviewerSystemPrompt = `You are a code review engineer. You receive a GitHub issue, research context, and an implementation plan. Verify the plan is correct.

Check:
1. Does the plan address the issue?
2. Are the file paths valid (they should match paths in the research context)?
3. Are the code changes syntactically correct Go?
4. Are the changes minimal and focused?

Output ONLY valid JSON:
{
  "approved": true/false,
  "feedback": "explanation of issues found, or 'Plan looks good' if approved"
}`

// ReviewPlan calls Gemini to validate an implementation plan against the issue and dossier.
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
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return nil, fmt.Errorf("parsing review JSON: %w (raw: %.200s)", err, text)
	}

	return &result, nil
}

func buildReviewerPrompt(issueTitle, issueBody string, dossier *archivist.Dossier, plan *ImplementationPlan) string {
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
		fmt.Fprintf(&b, "### %s\n\nReason: %s\n\n```\n%s\n```\n\n", f.Path, f.Reason, f.Content)
	}

	b.WriteString("## Implementation Plan\n\n")
	fmt.Fprintf(&b, "Summary: %s\n\n", plan.Summary)
	for _, c := range plan.Changes {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n```\n%s\n```\n\n", c.Path, c.Description, c.Content)
	}

	if len(plan.Verification) > 0 {
		b.WriteString("## Verification Commands\n\n")
		for _, v := range plan.Verification {
			fmt.Fprintf(&b, "- `%s`\n", v)
		}
	}

	return b.String()
}
