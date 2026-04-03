package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// VerifierReport summarizes the results of running validation commands.
type VerifierReport struct {
	Commands    []models.CommandLog `json:"commands"`
	OverallPass bool                `json:"overall_pass"`
	Summary     string              `json:"summary"`
}

// Verify runs each command from the dossier and collects results.
func Verify(ctx context.Context, runner *execution.CommandRunner, dossier models.Dossier) VerifierReport {
	if len(dossier.LikelyCommands) == 0 {
		return VerifierReport{
			OverallPass: true,
			Summary:     "no commands to run",
		}
	}

	var commands []models.CommandLog
	var failed []string
	for _, cmd := range dossier.LikelyCommands {
		log := runner.Run(ctx, cmd)
		commands = append(commands, log)
		if log.ExitCode != 0 {
			failed = append(failed, cmd)
		}
	}

	pass := len(failed) == 0
	total := len(dossier.LikelyCommands)
	passed := total - len(failed)

	var summary string
	if pass {
		summary = fmt.Sprintf("%d/%d commands passed", passed, total)
	} else {
		summary = fmt.Sprintf("%d/%d commands failed: %s", len(failed), total, strings.Join(failed, ", "))
	}

	return VerifierReport{
		Commands:    commands,
		OverallPass: pass,
		Summary:     summary,
	}
}
