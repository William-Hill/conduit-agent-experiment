package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"google.golang.org/genai"
)

const plannerSystemPrompt = `You are a senior Go engineer writing an implementation plan. You receive a GitHub issue and research context (relevant file contents). Your job is to write the EXACT new file contents for each file that needs to change.

Output ONLY valid JSON with this schema:
{
  "summary": "one-line description of what the plan does",
  "changes": [
    {
      "path": "relative/path/to/file.go",
      "description": "what changed and why",
      "content": "complete new file content"
    }
  ],
  "verification": ["go build ./...", "go vet ./..."]
}

Rules:
- Write COMPLETE file contents, not diffs. The implementer will overwrite the entire file.
- Make minimal changes. Only modify what's needed to fix the issue.
- Keep all existing code that doesn't need to change.
- Follow the existing code style exactly.
- Include relevant verification commands.`

// CreatePlan calls Gemini to produce an implementation plan with exact file contents.
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
	})
	if err != nil {
		return nil, fmt.Errorf("generating plan: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from model")
	}

	text := resp.Candidates[0].Content.Parts[0].Text

	var plan ImplementationPlan
	if err := json.Unmarshal([]byte(cleanJSON(text)), &plan); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w (raw: %.200s)", err, text)
	}

	return &plan, nil
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
		fmt.Fprintf(&b, "### %s\n\nReason: %s\n\n```\n%s\n```\n\n", f.Path, f.Reason, f.Content)
	}

	return b.String()
}

// cleanJSON strips markdown code fences and trims whitespace from model output.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip ```json ... ``` fences
	if strings.HasPrefix(s, "```") {
		// Remove opening fence line
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		// Remove closing fence
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
