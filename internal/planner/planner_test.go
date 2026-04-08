package planner

import (
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llmutil"
)

var dossierFixture = archivist.Dossier{
	Summary:  "The bug is in main.go line 42",
	Approach: "Fix the nil check",
	Risks:    []string{"might break tests"},
	Files: []archivist.FileEntry{
		{Path: "main.go", Reason: "contains the bug", Content: "package main\n\nfunc main() {}"},
	},
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "markdown json fence",
			input: "```json\n{\"summary\":\"test\"}\n```",
			want:  "{\"summary\":\"test\"}",
		},
		{
			name:  "plain json",
			input: "{\"summary\":\"test\"}",
			want:  "{\"summary\":\"test\"}",
		},
		{
			name:  "whitespace padded",
			input: "  \n{\"summary\":\"test\"}\n  ",
			want:  "{\"summary\":\"test\"}",
		},
		{
			name:  "markdown fence no language",
			input: "```\n{\"approved\":true}\n```",
			want:  "{\"approved\":true}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := llmutil.CleanJSON(tt.input)
			if got != tt.want {
				t.Errorf("llmutil.CleanJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPlannerPrompt(t *testing.T) {
	// Smoke test: just ensure it doesn't panic and includes key content.
	prompt := buildPlannerPrompt("Fix bug", "The thing is broken", &dossierFixture)
	if len(prompt) == 0 {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "Fix bug") {
		t.Error("prompt should contain issue title")
	}
	if !strings.Contains(prompt, "The thing is broken") {
		t.Error("prompt should contain issue body")
	}
}

func TestBuildReviewerPrompt(t *testing.T) {
	plan := &ImplementationPlan{
		Markdown: "# Fix the bug\n\nChange main.go to fix the nil check.",
	}
	prompt := buildReviewerPrompt("Fix bug", "The thing is broken", &dossierFixture, plan)
	if !strings.Contains(prompt, "Implementation Plan") {
		t.Error("reviewer prompt should contain plan section")
	}
	if !strings.Contains(prompt, "Fix the bug") {
		t.Error("reviewer prompt should contain plan content")
	}
}

