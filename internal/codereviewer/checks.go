package codereviewer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// checkTimeout is the hard cap for a single deterministic check.
// Matches internal/implementer/tools.go:268 (run_command tool).
const checkTimeout = 2 * time.Minute

// maxCheckOutput caps combined stdout+stderr at 16 KiB so pathological
// build logs can't blow up the LLM prompt or run-summary artifact.
const maxCheckOutput = 16 * 1024

// runGo executes `go <sub> ./...` in repoDir with a bounded environment
// and timeout. The minimal env mirrors internal/implementer/tools.go:275-281
// to prevent a compromised target repo from exfiltrating API keys.
func runGo(ctx context.Context, repoDir, sub string) (*CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", sub, "./...")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"GOROOT=" + os.Getenv("GOROOT"),
		"TMPDIR=" + os.TempDir(),
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Deadline exceeded is a runner error, not a verdict.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("go %s ./... timed out after %s", sub, checkTimeout)
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., go binary missing) — bubble up.
			return nil, fmt.Errorf("running go %s: %w", sub, runErr)
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	if len(output) > maxCheckOutput {
		output = output[:maxCheckOutput] + "\n... (truncated)"
	}

	return &CheckResult{
		Passed:   exitCode == 0,
		ExitCode: exitCode,
		Output:   output,
	}, nil
}

// RunBuild executes `go build ./...` in repoDir.
func RunBuild(ctx context.Context, repoDir string) (*CheckResult, error) {
	return runGo(ctx, repoDir, "build")
}
