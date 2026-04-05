package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestArchitectReviewApprove(t *testing.T) {
	llmResponse := `{
		"recommendation": "approve",
		"confidence": "high",
		"alignment_notes": "The patch stays within the pipeline subsystem boundaries.",
		"risks_identified": [],
		"adr_conflicts": [],
		"suggestions": ["Add a unit test for edge case X"],
		"rationale": "The patch is minimal, well-scoped, and aligns with the existing architecture."
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	input := ArchitectInput{
		Diff: "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new",
		Dossier: models.Dossier{
			TaskID:  "task-001",
			Summary: "Fix pipeline config",
			Risks:   []string{"low blast radius"},
		},
		Plan: PatchPlan{
			PlanSummary:   "Update pipeline config parsing",
			DesignChoices: []string{"Use existing parser interface"},
			Assumptions:   []string{"No breaking changes to public API"},
		},
		VerifierReport: VerifierReport{
			OverallPass: true,
			Summary:     "2/2 commands passed",
		},
		SupplementalDocs: map[string]string{
			"docs/adr/001-pipelines.md": "# ADR 001\nPipeline config must be backwards-compatible.",
		},
	}

	result, llmCall, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", input)
	if err != nil {
		t.Fatalf("ArchitectReview() error: %v", err)
	}

	if result.Recommendation != "approve" {
		t.Errorf("recommendation = %q, want approve", result.Recommendation)
	}
	if result.Confidence != "high" {
		t.Errorf("confidence = %q, want high", result.Confidence)
	}
	if llmCall.Agent != "architect" {
		t.Errorf("llm call agent = %q, want architect", llmCall.Agent)
	}
}

func TestArchitectReviewReject(t *testing.T) {
	llmResponse := `{
		"recommendation": "reject",
		"confidence": "high",
		"alignment_notes": "The patch crosses subsystem boundaries.",
		"risks_identified": ["Breaks public API contract", "Concurrent access risk"],
		"adr_conflicts": ["ADR-003: No direct DB access from handlers"],
		"suggestions": [],
		"rationale": "The patch introduces a dependency that violates the layered architecture and conflicts with ADR-003."
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	input := ArchitectInput{
		Diff: "--- a/handler.go\n+++ b/handler.go\n@@ -1 +1 @@\n-old\n+new",
		Dossier: models.Dossier{
			TaskID:  "task-002",
			Summary: "Add direct DB query from handler",
		},
		Plan: PatchPlan{
			PlanSummary: "Add DB query in HTTP handler",
		},
		VerifierReport: VerifierReport{
			OverallPass: false,
			Summary:     "1/2 commands failed",
		},
	}

	result, _, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", input)
	if err != nil {
		t.Fatalf("ArchitectReview() error: %v", err)
	}

	if result.Recommendation != "reject" {
		t.Errorf("recommendation = %q, want reject", result.Recommendation)
	}
}

func TestArchitectReviewBadJSON(t *testing.T) {
	server := mockLLMServer(t, "this is not valid json at all")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	input := ArchitectInput{
		Diff:    "--- a/foo.go\n+++ b/foo.go",
		Dossier: models.Dossier{TaskID: "task-003"},
		Plan:    PatchPlan{PlanSummary: "some plan"},
		VerifierReport: VerifierReport{
			OverallPass: true,
			Summary:     "all good",
		},
	}

	_, _, err := ArchitectReview(context.Background(), client, "gemini-2.5-flash", input)
	if err == nil {
		t.Fatal("expected error on bad JSON, got nil")
	}
}
