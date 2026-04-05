package orchestrator

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCheckTaskPass(t *testing.T) {
	policy := DefaultPhase1Policy()
	task := models.Task{Difficulty: models.DifficultyL1, BlastRadius: models.BlastRadiusLow}
	if err := policy.CheckTask(task); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestCheckTaskDifficultyExceeded(t *testing.T) {
	policy := DefaultPhase1Policy()
	task := models.Task{Difficulty: models.DifficultyL3, BlastRadius: models.BlastRadiusLow}
	if err := policy.CheckTask(task); err == nil {
		t.Error("expected error for L3 task")
	}
}

func TestCheckPatchBreadthPass(t *testing.T) {
	policy := DefaultPhase1Policy()
	if err := policy.CheckPatchBreadth(5); err != nil {
		t.Errorf("expected pass for 5 files, got: %v", err)
	}
}

func TestCheckPatchBreadthExceeded(t *testing.T) {
	policy := DefaultPhase1Policy()
	if err := policy.CheckPatchBreadth(15); err == nil {
		t.Error("expected error for 15 files when max is 10")
	}
}
