package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const selectorSystemPrompt = `You are a maintenance task selector for an open source project. Given a list of GitHub issues, rank them by suitability for automated narrow fixes.

Criteria:
- Is this a bug fix, docs issue, config mismatch, dependency bump, or narrow improvement?
- Can it be resolved with changes to 5 or fewer files?
- Are reproduction steps or acceptance criteria clear?
- Estimated difficulty: L1 (docs/deps/lint), L2 (narrow bug fix, config alignment), L3 (contained features), L4 (runtime semantics)
- Estimated blast radius: low, medium, high

Return a JSON array of objects, one per issue, ranked from most to least suitable:
[{"number": 123, "difficulty": "L1", "blast_radius": "low", "rationale": "...", "acceptance_criteria": ["..."]}]

Only include issues that are L1 or L2 difficulty. Exclude L3+ issues entirely.
Respond ONLY with the JSON array, no markdown fences or extra text.`

// excludedLabels are labels that indicate an issue should not be selected.
var excludedLabels = map[string]bool{
	"epic": true, "arch-v2": true, "wontfix": true, "duplicate": true,
}

// excludedKeywords in issue body indicate the issue is too broad.
var excludedKeywords = []string{"redesign", "breaking change", "rewrite", "refactor entire"}

// RankedIssue is an LLM-ranked GitHub issue with metadata.
type RankedIssue struct {
	Number             int      `json:"number"`
	Difficulty         string   `json:"difficulty"`
	BlastRadius        string   `json:"blast_radius"`
	Rationale          string   `json:"rationale"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// FilterIssues applies heuristic pre-filters to exclude issues that are clearly out of scope.
func FilterIssues(issues []github.Issue) []github.Issue {
	var filtered []github.Issue
	for _, issue := range issues {
		if len(issue.Assignees) > 0 {
			continue
		}

		excluded := false
		for _, label := range issue.Labels {
			if excludedLabels[label.Name] {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		body := strings.ToLower(issue.Body)
		for _, kw := range excludedKeywords {
			if strings.Contains(body, kw) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		if len(issue.Body) > 2000 {
			continue
		}

		filtered = append(filtered, issue)
	}
	return filtered
}

// RankIssues uses the LLM to rank filtered issues by suitability.
func RankIssues(ctx context.Context, client *llm.Client, modelName string, issues []github.Issue) ([]RankedIssue, models.LLMCall, error) {
	userPrompt := buildSelectorPrompt(issues)

	start := time.Now()
	response, err := client.Complete(ctx, selectorSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "selector",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return nil, call, fmt.Errorf("selector LLM call failed: %w", err)
	}

	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var ranked []RankedIssue
	if err := json.Unmarshal([]byte(cleaned), &ranked); err != nil {
		return nil, call, fmt.Errorf("parsing ranked issues JSON: %w", err)
	}

	return ranked, call, nil
}

// RankedToTask converts a RankedIssue and its GitHub Issue into a Task.
func RankedToTask(ranked RankedIssue, issue github.Issue) models.Task {
	return models.Task{
		ID:                 fmt.Sprintf("task-gh-%d", ranked.Number),
		Title:              issue.Title,
		Source:             fmt.Sprintf("github#%d", ranked.Number),
		Description:        issue.Body,
		Difficulty:         models.Difficulty(ranked.Difficulty),
		BlastRadius:        models.BlastRadius(ranked.BlastRadius),
		AcceptanceCriteria: ranked.AcceptanceCriteria,
		IssueNumber:        ranked.Number,
		Status:             models.TaskStatusPending,
	}
}

func buildSelectorPrompt(issues []github.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## GitHub Issues (%d total)\n\n", len(issues))
	for _, issue := range issues {
		fmt.Fprintf(&b, "### #%d: %s\n", issue.Number, issue.Title)
		var labelNames []string
		for _, l := range issue.Labels {
			labelNames = append(labelNames, l.Name)
		}
		if len(labelNames) > 0 {
			fmt.Fprintf(&b, "Labels: %s\n", strings.Join(labelNames, ", "))
		}
		fmt.Fprintf(&b, "%s\n\n", issue.Body)
	}
	return b.String()
}
