package reporting

import (
	"strings"
	"testing"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestRenderMarkdownWithM2Fields(t *testing.T) {
	run := models.Run{
		ID:                "run-m2-001",
		TaskID:            "task-002",
		StartedAt:         time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC),
		EndedAt:           time.Date(2026, 4, 3, 10, 30, 0, 0, time.UTC),
		AgentsInvoked:     []string{"implementer", "architect"},
		FinalStatus:       models.RunStatusSuccess,
		HumanDecision:     models.HumanDecisionApproved,
		ImplementerPlan:   "1. Edit foo.go\n2. Run tests\n3. Open PR",
		ImplementerDiff:   "diff --git a/foo.go b/foo.go\n+func NewThing() {}",
		ArchitectDecision: "approved",
		ArchitectReview:   "Looks good. No structural concerns.",
		PRURL:             "https://github.com/org/repo/pull/42",
	}

	dossier := models.Dossier{
		TaskID:  "task-002",
		Summary: "A simple M2 task.",
	}

	task := models.Task{
		ID:          "task-002",
		Title:       "Add new feature",
		Description: "Implement the new thing.",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}

	md, err := RenderMarkdown(run, dossier, task)
	if err != nil {
		t.Fatalf("RenderMarkdown() error: %v", err)
	}

	checks := []string{
		"## Patch Plan",
		"1. Edit foo.go",
		"## Diff",
		"diff --git a/foo.go",
		"## Architect Review",
		"approved",
		"Looks good. No structural concerns.",
		"## Pull Request",
		"https://github.com/org/repo/pull/42",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestRenderMarkdown(t *testing.T) {
	pass := true
	run := models.Run{
		ID:              "run-001",
		TaskID:          "task-001",
		StartedAt:       time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
		EndedAt:         time.Date(2026, 4, 2, 12, 5, 0, 0, time.UTC),
		AgentsInvoked:   []string{"archivist", "triage", "verifier"},
		FinalStatus:     models.RunStatusSuccess,
		HumanDecision:   models.HumanDecisionPending,
		TriageDecision:  "accept",
		TriageReason:    "task within policy limits",
		VerifierPass:    &pass,
		VerifierSummary: "2/2 commands passed",
		CommandsRun: []models.CommandLog{
			{Command: "make test", ExitCode: 0, Stdout: "ok"},
			{Command: "go build ./...", ExitCode: 0, Stdout: ""},
		},
	}

	dossier := models.Dossier{
		TaskID:         "task-001",
		Summary:        "LLM-enhanced summary of the task",
		RelatedFiles:   []string{"docs/pipeline-config.md", "internal/pipeline/config.go"},
		RelatedDocs:    []string{"docs/design-documents/001-pipelines.md"},
		LikelyCommands: []string{"make test", "go build ./..."},
		Risks:          []string{"No major risks"},
		OpenQuestions:  []string{"Are all affected files identified?"},
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config example",
		Description: "Update docs to match current config behavior.",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}

	md, err := RenderMarkdown(run, dossier, task)
	if err != nil {
		t.Fatalf("RenderMarkdown() error: %v", err)
	}

	checks := []string{
		"# Run Report: run-001",
		"## Task",
		"task-001",
		"Fix docs drift in pipeline config example",
		"## Dossier",
		"LLM-enhanced summary",
		"docs/pipeline-config.md",
		"docs/design-documents/001-pipelines.md",
		"## Likely Commands",
		"make test",
		"## Risks",
		"## Open Questions",
		"## Triage",
		"accept",
		"task within policy limits",
		"## Verification",
		"2/2 commands passed",
		"## Run Details",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}
