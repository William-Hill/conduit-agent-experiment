package responder

import (
	"testing"
)

func TestParseInlineComments(t *testing.T) {
	raw := `[
		{
			"user": {"login": "greptile-apps[bot]"},
			"path": "internal/cost/budget.go",
			"line": 27,
			"body": "P1 **PLANNER_MAX_COST is loaded but never enforced**\nNo CheckStep call exists."
		},
		{
			"user": {"login": "coderabbitai[bot]"},
			"path": "internal/archivist/dossier_test.go",
			"line": 38,
			"body": "_⚠️ Potential issue_ | _🟠 Major_\n\n**Avoid potential index panic.**\n\nUse t.Fatalf instead of t.Errorf."
		},
		{
			"user": {"login": "chatgpt-codex-connector[bot]"},
			"path": "cmd/implementer/main.go",
			"line": 141,
			"body": "![P2 Badge](https://img.shields.io/badge/P2-yellow) **Stop CLI flow when budget exceeded**\nRunAgent returns BudgetExceeded but caller ignores it."
		},
		{
			"user": {"login": "coderabbitai[bot]"},
			"path": "docs/JOURNEY.md",
			"line": 144,
			"body": "✅ Addressed in commit 61404af"
		}
	]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatalf("ParseInlineComments: %v", err)
	}
	if len(comments) != 4 {
		t.Fatalf("got %d comments, want 4", len(comments))
	}
	if comments[0].Author != "greptile-apps[bot]" {
		t.Errorf("Author = %q, want greptile-apps[bot]", comments[0].Author)
	}
	if comments[0].File != "internal/cost/budget.go" {
		t.Errorf("File = %q", comments[0].File)
	}
	if comments[0].Line != 27 {
		t.Errorf("Line = %d, want 27", comments[0].Line)
	}
	if comments[3].Status != "addressed" {
		t.Errorf("comment with 'Addressed in commit' should have status=addressed, got %q", comments[3].Status)
	}
}

func TestParseReviews(t *testing.T) {
	raw := `[
		{"author": {"login": "user1"}, "state": "APPROVED"},
		{"author": {"login": "coderabbitai"}, "state": "COMMENTED"}
	]`
	approved, err := HasApproval([]byte(raw))
	if err != nil {
		t.Fatalf("HasApproval: %v", err)
	}
	if !approved {
		t.Error("expected approved=true when APPROVED review exists")
	}
}

func TestParseReviewsNoApproval(t *testing.T) {
	raw := `[
		{"author": {"login": "coderabbitai"}, "state": "COMMENTED"},
		{"author": {"login": "greptile-apps"}, "state": "COMMENTED"}
	]`
	approved, err := HasApproval([]byte(raw))
	if err != nil {
		t.Fatalf("HasApproval: %v", err)
	}
	if approved {
		t.Error("expected approved=false when no APPROVED review")
	}
}

func TestParseReviewsChangesRequestedSupersedes(t *testing.T) {
	raw := `[
		{"author": {"login": "user1"}, "state": "APPROVED"},
		{"author": {"login": "user1"}, "state": "CHANGES_REQUESTED"}
	]`
	approved, err := HasApproval([]byte(raw))
	if err != nil {
		t.Fatalf("HasApproval: %v", err)
	}
	if approved {
		t.Error("expected approved=false when latest review is CHANGES_REQUESTED")
	}
}

func TestGreptileCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "greptile-apps[bot]"},
		"path": "internal/cost/budget.go",
		"line": 27,
		"body": "<a href=\"#\"><img alt=\"P1\" src=\"https://greptile-static-assets.s3.amazonaws.com/badges/p1.svg\" align=\"top\"></a> **PLANNER_MAX_COST is loaded but never enforced**\n\nLoadBudget reads PLANNER_MAX_COST but no CheckStep call exists."
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "critical" {
		t.Errorf("Greptile P1 should be critical, got %q", actionable[0].Severity)
	}
}

func TestCodeRabbitCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "coderabbitai[bot]"},
		"path": "internal/archivist/dossier_test.go",
		"line": 38,
		"body": "_⚠️ Potential issue_ | _🟠 Major_\n\n**Avoid potential index panic.**"
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "major" {
		t.Errorf("CodeRabbit Major should be major, got %q", actionable[0].Severity)
	}
}

func TestCodexCommentSeverity(t *testing.T) {
	raw := `[{
		"user": {"login": "chatgpt-codex-connector[bot]"},
		"path": "cmd/implementer/main.go",
		"line": 141,
		"body": "**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Stop CLI flow when budget exceeded**"
	}]`

	comments, err := ParseInlineComments([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	actionable := Classify(comments)
	if len(actionable) != 1 {
		t.Fatalf("got %d, want 1", len(actionable))
	}
	if actionable[0].Severity != "major" {
		t.Errorf("Codex P2 should be major, got %q", actionable[0].Severity)
	}
}
