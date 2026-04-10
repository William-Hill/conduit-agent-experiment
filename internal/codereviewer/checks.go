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

// cappedBuffer is an io.Writer that accumulates up to cap bytes and
// silently drops writes past the cap while continuing to return success
// from Write. This bounds the memory footprint of runGo's stdout/stderr
// capture — without it, a pathological `go build` error flood could
// buffer hundreds of KiB before the post-run truncation kicked in.
//
// Write returns len(p), nil even when bytes are dropped so that the
// exec.Cmd io.Copy loop does not fail with io.ErrShortWrite.
type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.cap - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	c.buf.Write(p[:remaining])
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) String() string  { return c.buf.String() }
func (c *cappedBuffer) Len() int        { return c.buf.Len() }
func (c *cappedBuffer) Truncated() bool { return c.truncated }

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

	// Cap each stream at maxCheckOutput so memory stays bounded during
	// capture, not just after the fact. Combined post-run output still
	// gets truncated as a safety net below.
	stdout := &cappedBuffer{cap: maxCheckOutput}
	stderr := &cappedBuffer{cap: maxCheckOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

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
	// Truncation signal comes from two sources: a per-stream cap hit
	// during capture (stdout/stderr.Truncated) or the combined output
	// exceeding the cap after concatenation. Either way, cap the
	// combined output and append a single marker.
	wasTruncated := stdout.Truncated() || stderr.Truncated() || len(output) > maxCheckOutput
	if len(output) > maxCheckOutput {
		output = output[:maxCheckOutput]
	}
	if wasTruncated {
		output += "\n... (truncated)"
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

// RunVet executes `go vet ./...` in repoDir.
func RunVet(ctx context.Context, repoDir string) (*CheckResult, error) {
	return runGo(ctx, repoDir, "vet")
}
