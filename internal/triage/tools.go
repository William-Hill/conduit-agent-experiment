package triage

import (
	"fmt"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// Tool input/output types

// ListIssuesInput is the argument for the list_issues tool.
type ListIssuesInput struct {
	Limit  int      `json:"limit"`
	Labels []string `json:"labels,omitempty"`
}

// ListIssuesOutput is the result of the list_issues tool.
type ListIssuesOutput struct {
	Issues []IssueInfo `json:"issues"`
	Total  int         `json:"total"`
}

// GetIssueInput is the argument for the get_issue tool.
type GetIssueInput struct {
	Number int `json:"number"`
}

// GetIssueOutput is the result of the get_issue tool.
type GetIssueOutput struct {
	Issue IssueDetail `json:"issue"`
}

// SaveRankingInput is the argument for the save_ranking tool.
type SaveRankingInput struct {
	Ranked []RankedIssue `json:"ranked"`
}

// SaveRankingOutput is the result of the save_ranking tool.
type SaveRankingOutput struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// labelNames extracts label name strings from github.Label slices.
func labelNames(labels []github.Label) []string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names
}

// NewTools creates the three function tools for the triage agent.
func NewTools(adapter *github.Adapter, outputDir string) ([]tool.Tool, error) {
	repo := adapter.Owner + "/" + adapter.Repo

	listTool, err := functiontool.New(functiontool.Config{
		Name:        "list_issues",
		Description: "List open GitHub issues for the target repository. Returns issue number, title, labels, body preview (first 500 chars), comment count, and assignee count. Use limit to control how many issues to fetch (default 50, max 200).",
	}, func(ctx tool.Context, input ListIssuesInput) (ListIssuesOutput, error) {
		issues, err := adapter.ListIssues(ctx, github.IssueListOpts{
			Limit:  input.Limit,
			Labels: input.Labels,
		})
		if err != nil {
			return ListIssuesOutput{}, fmt.Errorf("listing issues: %w", err)
		}

		infos := make([]IssueInfo, len(issues))
		for i, iss := range issues {
			body := iss.Body
			if len(body) > 500 {
				body = body[:500] + "..."
			}
			infos[i] = IssueInfo{
				Number:     iss.Number,
				Title:      iss.Title,
				Labels:     labelNames(iss.Labels),
				BodyPrefix: body,
				Comments:   len(iss.Comments),
				Assignees:  len(iss.Assignees),
				CreatedAt:  iss.CreatedAt,
			}
		}

		return ListIssuesOutput{Issues: infos, Total: len(infos)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating list_issues tool: %w", err)
	}

	getTool, err := functiontool.New(functiontool.Config{
		Name:        "get_issue",
		Description: "Get full details for a single GitHub issue by number. Returns the complete body, all labels, comment count, and assignee count. Use this to get more context on promising issues identified by list_issues.",
	}, func(ctx tool.Context, input GetIssueInput) (GetIssueOutput, error) {
		issue, err := adapter.GetIssue(ctx, input.Number)
		if err != nil {
			return GetIssueOutput{}, fmt.Errorf("getting issue %d: %w", input.Number, err)
		}

		return GetIssueOutput{
			Issue: IssueDetail{
				Number:    issue.Number,
				Title:     issue.Title,
				Labels:    labelNames(issue.Labels),
				Body:      issue.Body,
				Comments:  len(issue.Comments),
				Assignees: len(issue.Assignees),
				CreatedAt: issue.CreatedAt,
			},
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating get_issue tool: %w", err)
	}

	saveTool, err := functiontool.New(functiontool.Config{
		Name:        "save_ranking",
		Description: "Save your final ranked list of issues. Call this once you have classified and ranked all issues. Provide the ranked list sorted by score (highest first). Only include issues suitable for automated maintenance (bugs, housekeeping, docs with clear scope).",
	}, func(_ tool.Context, input SaveRankingInput) (SaveRankingOutput, error) {
		output := TriageOutput{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Repo:        repo,
			RankedCount: len(input.Ranked),
			Ranked:      input.Ranked,
		}

		path, err := SaveRanking(outputDir, output)
		if err != nil {
			return SaveRankingOutput{}, err
		}

		return SaveRankingOutput{
			Path:  path,
			Count: len(input.Ranked),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating save_ranking tool: %w", err)
	}

	return []tool.Tool{listTool, getTool, saveTool}, nil
}
