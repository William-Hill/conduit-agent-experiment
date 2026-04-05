package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestFilterIssues(t *testing.T) {
	issues := []github.Issue{
		{Number: 1, Title: "Simple bug", Body: "short description", Labels: []github.Label{{Name: "bug"}}},
		{Number: 2, Title: "Epic rewrite", Body: "redesign the entire system with breaking changes", Labels: []github.Label{{Name: "epic"}}},
		{Number: 3, Title: "Assigned issue", Body: "already taken", Assignees: []any{"someone"}},
		{Number: 4, Title: "Docs fix", Body: "update readme", Labels: []github.Label{{Name: "docs"}}},
	}

	filtered := FilterIssues(issues)
	if len(filtered) != 2 {
		t.Errorf("got %d issues, want 2 (should exclude epic and assigned)", len(filtered))
	}
	for _, f := range filtered {
		if f.Number == 2 {
			t.Error("should have filtered out epic issue")
		}
		if f.Number == 3 {
			t.Error("should have filtered out assigned issue")
		}
	}
}

func TestRankIssues(t *testing.T) {
	llmResponse := `[
		{"number": 1, "difficulty": "L1", "blast_radius": "low", "rationale": "Simple bug fix", "acceptance_criteria": ["Fix the bug"]},
		{"number": 4, "difficulty": "L1", "blast_radius": "low", "rationale": "Docs update", "acceptance_criteria": ["Update docs"]}
	]`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	issues := []github.Issue{
		{Number: 1, Title: "Simple bug", Body: "short"},
		{Number: 4, Title: "Docs fix", Body: "update"},
	}

	ranked, _, err := RankIssues(context.Background(), client, "gemini-2.5-flash", issues)
	if err != nil {
		t.Fatalf("RankIssues() error: %v", err)
	}
	if len(ranked) != 2 {
		t.Errorf("got %d ranked, want 2", len(ranked))
	}
	if ranked[0].Number != 1 {
		t.Errorf("first ranked number = %d, want 1", ranked[0].Number)
	}
}

func TestRankedToTask(t *testing.T) {
	ranked := RankedIssue{
		Number:             123,
		Difficulty:         "L1",
		BlastRadius:        "low",
		AcceptanceCriteria: []string{"Fix it"},
	}
	issue := github.Issue{
		Number: 123,
		Title:  "Bug in parsing",
		Body:   "Parsing fails on edge case",
	}

	task := RankedToTask(ranked, issue)
	if task.ID != "task-gh-123" {
		t.Errorf("task ID = %q, want task-gh-123", task.ID)
	}
	if task.IssueNumber != 123 {
		t.Errorf("issue number = %d, want 123", task.IssueNumber)
	}
	if task.Difficulty != models.DifficultyL1 {
		t.Errorf("difficulty = %q, want L1", task.Difficulty)
	}
	if task.BlastRadius != models.BlastRadiusLow {
		t.Errorf("BlastRadius = %q, want low", task.BlastRadius)
	}
	if len(task.AcceptanceCriteria) != 1 || task.AcceptanceCriteria[0] != "Fix it" {
		t.Errorf("AcceptanceCriteria = %v, want [Fix it]", task.AcceptanceCriteria)
	}
}
