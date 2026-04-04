package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// VerifierReport summarizes the results of running validation commands.
type VerifierReport struct {
	Commands    []models.CommandLog `json:"commands"`
	OverallPass bool                `json:"overall_pass"`
	Summary     string              `json:"summary"`
}

// allowedCommandPrefixes defines which commands the verifier is permitted to run.
// Commands not matching any prefix are blocked to prevent LLM-influenced injection.
var allowedCommandPrefixes = []string{
	"go test", "go vet", "go build",
	"make", "grep",
	"golangci-lint",
	"echo",
}

func isAllowedCommand(cmd string) bool {
	c := strings.TrimSpace(cmd)
	for _, p := range allowedCommandPrefixes {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
}

// Verify runs each command from the dossier and collects results.
// Commands not matching the allowlist are blocked and recorded as failures.
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
		if !isAllowedCommand(cmd) {
			commands = append(commands, models.CommandLog{
				Command:  cmd,
				ExitCode: -1,
				Stderr:   "blocked by verifier policy: command not allowlisted",
				RunAt:    time.Now(),
			})
			failed = append(failed, cmd)
			continue
		}
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
