package agents_test

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
)

func TestTriageAccept(t *testing.T) {
	task := models.Task{
		ID:          "task-001",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{
		TaskID:       "task-001",
		RelatedFiles: []string{"README.md"},
	}
	policy := orchestrator.DefaultPhase1Policy()

	decision := agents.Triage(task, dossier, policy)
	if decision.Decision != agents.DecisionAccept {
		t.Errorf("decision = %q, want accept", decision.Decision)
	}
}

func TestTriageRejectDifficulty(t *testing.T) {
	task := models.Task{
		ID:          "task-002",
		Difficulty:  models.DifficultyL4,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{TaskID: "task-002"}
	policy := orchestrator.DefaultPhase1Policy()

	decision := agents.Triage(task, dossier, policy)
	if decision.Decision != agents.DecisionReject {
		t.Errorf("decision = %q, want reject", decision.Decision)
	}
}

func TestTriageRejectBlastRadius(t *testing.T) {
	task := models.Task{
		ID:          "task-003",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusHigh,
	}
	dossier := models.Dossier{TaskID: "task-003"}
	policy := orchestrator.DefaultPhase1Policy()

	decision := agents.Triage(task, dossier, policy)
	if decision.Decision != agents.DecisionReject {
		t.Errorf("decision = %q, want reject", decision.Decision)
	}
}

func TestTriageDefer(t *testing.T) {
	task := models.Task{
		ID:          "task-004",
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}
	dossier := models.Dossier{
		TaskID:        "task-004",
		RelatedFiles:  nil,
		OpenQuestions: []string{"What files are affected?"},
	}
	policy := orchestrator.DefaultPhase1Policy()

	decision := agents.Triage(task, dossier, policy)
	if decision.Decision != agents.DecisionDefer {
		t.Errorf("decision = %q, want defer", decision.Decision)
	}
}
