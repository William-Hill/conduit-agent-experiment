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
		LikelyCommands: []string{"echo test1", "echo test2"},
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
		LikelyCommands: []string{"echo ok", "rm -rf /should-be-blocked", "echo after"},
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

func TestClassifyResults(t *testing.T) {
	baseline := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
		{Command: "go vet ./...", ExitCode: 1}, // pre-existing failure
	}
	postPatch := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 1}, // new failure from patch
		{Command: "go vet ./...", ExitCode: 1},   // same as baseline
	}
	patchFails, envFails := ClassifyResults(baseline, postPatch)
	if len(patchFails) != 1 || patchFails[0] != "go build ./..." {
		t.Errorf("patch failures = %v, want [go build ./...]", patchFails)
	}
	if len(envFails) != 1 || envFails[0] != "go vet ./..." {
		t.Errorf("env failures = %v, want [go vet ./...]", envFails)
	}
}

func TestClassifyResultsAllPass(t *testing.T) {
	baseline := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
	}
	postPatch := []models.CommandLog{
		{Command: "go build ./...", ExitCode: 0},
	}
	patchFails, envFails := ClassifyResults(baseline, postPatch)
	if len(patchFails) != 0 {
		t.Errorf("expected no patch failures, got %v", patchFails)
	}
	if len(envFails) != 0 {
		t.Errorf("expected no env failures, got %v", envFails)
	}
}
