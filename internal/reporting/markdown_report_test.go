package reporting

import (
	"strings"
	"testing"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestRenderMarkdown(t *testing.T) {
	run := models.Run{
		ID:            "run-001",
		TaskID:        "task-001",
		StartedAt:     time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 4, 2, 12, 5, 0, 0, time.UTC),
		AgentsInvoked: []string{"archivist"},
		FinalStatus:   models.RunStatusSuccess,
		HumanDecision: models.HumanDecisionPending,
	}

	dossier := models.Dossier{
		TaskID:         "task-001",
		Summary:        "Fix docs drift in pipeline config",
		RelatedFiles:   []string{"docs/pipeline-config.md", "internal/pipeline/config.go"},
		RelatedDocs:    []string{"docs/design-documents/001-pipelines.md"},
		LikelyCommands: []string{"make test", "go build ./..."},
		Risks:          []string{"No major risks"},
		OpenQuestions:   []string{"Are all affected files identified?"},
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

	// Check that key sections appear.
	checks := []string{
		"# Run Report: run-001",
		"## Task",
		"task-001",
		"Fix docs drift in pipeline config example",
		"## Dossier",
		"docs/pipeline-config.md",
		"docs/design-documents/001-pipelines.md",
		"## Likely Commands",
		"make test",
		"## Risks",
		"## Open Questions",
		"## Run Details",
	}
	for _, want := range checks {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}
