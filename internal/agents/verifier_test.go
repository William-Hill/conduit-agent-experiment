package agents

import (
	"context"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestVerifyAllPass(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: []string{"echo test1", "true"},
	}

	report := Verify(context.Background(), runner, dossier)
	if !report.OverallPass {
		t.Error("expected OverallPass=true when all commands succeed")
	}
	if len(report.Commands) != 2 {
		t.Errorf("commands count = %d, want 2", len(report.Commands))
	}
}

func TestVerifyWithFailure(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: []string{"true", "false", "echo after"},
	}

	report := Verify(context.Background(), runner, dossier)
	if report.OverallPass {
		t.Error("expected OverallPass=false when a command fails")
	}
	if len(report.Commands) != 3 {
		t.Errorf("commands count = %d, want 3 (should run all commands)", len(report.Commands))
	}
}

func TestVerifyNoCommands(t *testing.T) {
	runner := &execution.CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	dossier := models.Dossier{
		LikelyCommands: nil,
	}

	report := Verify(context.Background(), runner, dossier)
	if !report.OverallPass {
		t.Error("expected OverallPass=true when no commands to run")
	}
}
