package execution

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "echo hello world")
	if log.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", log.ExitCode)
	}
	if !strings.Contains(log.Stdout, "hello world") {
		t.Errorf("stdout = %q, want 'hello world'", log.Stdout)
	}
}

func TestRunFailure(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "false")
	if log.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestRunTimeout(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 1,
	}

	log := runner.Run(context.Background(), "sleep 30")
	if log.ExitCode != -1 {
		t.Errorf("exit code = %d, want -1 for timeout", log.ExitCode)
	}
	if !strings.Contains(log.Stderr, "timed out") {
		t.Errorf("stderr should mention timeout, got %q", log.Stderr)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	runner := &CommandRunner{
		WorkDir:        t.TempDir(),
		TimeoutSeconds: 10,
	}

	log := runner.Run(context.Background(), "echo error >&2")
	if !strings.Contains(log.Stderr, "error") {
		t.Errorf("stderr = %q, want 'error'", log.Stderr)
	}
}

func TestWorktreeSetupCleanup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	cmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"touch file.txt",
		"git add .",
		"git commit -m init",
	}
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %q failed: %v\n%s", c, err, out)
		}
	}

	runner := &CommandRunner{
		RepoPath:       repoDir,
		UseWorktree:    true,
		TimeoutSeconds: 10,
	}

	if err := runner.Setup(); err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	defer runner.Cleanup()

	if runner.WorkDir == repoDir {
		t.Error("WorkDir should differ from RepoPath when using worktree")
	}
	if _, err := os.Stat(runner.WorkDir); err != nil {
		t.Errorf("worktree dir should exist: %v", err)
	}

	log := runner.Run(context.Background(), "ls file.txt")
	if log.ExitCode != 0 {
		t.Errorf("file.txt should exist in worktree, exit=%d stderr=%q", log.ExitCode, log.Stderr)
	}

	if err := runner.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(runner.WorkDir); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after cleanup")
	}
}
