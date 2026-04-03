package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// CommandRunner executes shell commands with timeout and output capture.
type CommandRunner struct {
	WorkDir        string
	RepoPath       string
	UseWorktree    bool
	TimeoutSeconds int
	worktreePath   string
}

// Setup prepares the execution environment. If UseWorktree is true,
// creates a git worktree from RepoPath.
func (r *CommandRunner) Setup() error {
	if !r.UseWorktree {
		r.WorkDir = r.RepoPath
		return nil
	}

	wtDir, err := os.MkdirTemp("", "conduit-experiment-wt-*")
	if err != nil {
		return fmt.Errorf("creating temp dir for worktree: %w", err)
	}

	os.Remove(wtDir)

	cmd := exec.Command("git", "worktree", "add", "--detach", wtDir)
	cmd.Dir = r.RepoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating worktree: %w\n%s", err, out)
	}

	r.worktreePath = wtDir
	r.WorkDir = wtDir
	return nil
}

// Cleanup removes the worktree if one was created.
func (r *CommandRunner) Cleanup() error {
	if r.worktreePath == "" {
		return nil
	}

	os.RemoveAll(r.worktreePath)

	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = r.RepoPath
	cmd.Run()

	r.worktreePath = ""
	return nil
}

// Run executes a shell command and returns a CommandLog with captured output.
func (r *CommandRunner) Run(ctx context.Context, command string) models.CommandLog {
	timeout := time.Duration(r.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()

	log := models.CommandLog{
		Command: command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
		RunAt:   startTime,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.ExitCode = -1
			log.Stderr = log.Stderr + fmt.Sprintf("\ncommand timed out after %s", timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			log.ExitCode = exitErr.ExitCode()
		} else {
			log.ExitCode = -1
			log.Stderr = log.Stderr + fmt.Sprintf("\ncommand error: %v", err)
		}
	}

	return log
}
