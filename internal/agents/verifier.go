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
	Commands           []models.CommandLog `json:"commands"`
	OverallPass        bool                `json:"overall_pass"`
	Summary            string              `json:"summary"`
	PatchFailures      []string            `json:"patch_failures,omitempty"`
	EnvironmentFailures []string           `json:"environment_failures,omitempty"`
}

// allowedCommandPrefixes defines which commands the verifier is permitted to run.
// Commands not matching any prefix are blocked to prevent LLM-influenced injection.
var allowedCommandPrefixes = []string{
	"go test", "go vet", "go build",
	"make", "grep",
	"golangci-lint",
	"echo",
	"shellcheck", "yamllint", "actionlint",
	"test -f", "test -x",
	"cat",
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

// VerifyBaseline runs the same commands as Verify on an unpatched worktree to
// establish which commands already fail before the patch is applied.
func VerifyBaseline(ctx context.Context, runner *execution.CommandRunner, dossier models.Dossier) []models.CommandLog {
	var logs []models.CommandLog
	for _, cmd := range dossier.LikelyCommands {
		if !isAllowedCommand(cmd) {
			continue
		}
		log := runner.Run(ctx, cmd)
		logs = append(logs, log)
	}
	return logs
}

// ClassifyResults compares baseline and post-patch command results.
// A command that fails in both baseline and post-patch is classified as
// environmental. A command that passes in baseline but fails post-patch
// is classified as patch-caused.
func ClassifyResults(baseline, postPatch []models.CommandLog) (patchFailures, envFailures []string) {
	baselineStatus := make(map[string]int) // command -> exit code
	for _, bl := range baseline {
		baselineStatus[bl.Command] = bl.ExitCode
	}
	for _, pp := range postPatch {
		if pp.ExitCode != 0 {
			if blCode, ok := baselineStatus[pp.Command]; ok && blCode != 0 {
				envFailures = append(envFailures, pp.Command)
			} else {
				patchFailures = append(patchFailures, pp.Command)
			}
		}
	}
	return
}
