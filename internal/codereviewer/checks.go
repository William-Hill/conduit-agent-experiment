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

// runBoundedCmd builds and executes a command under the package's
// bounded environment (PATH/HOME/GOPATH/GOROOT/TMPDIR only, plus
// GOFLAGS=-mod=readonly and GOWORK=off), captures stdout/stderr to
// maxCheckOutput via cappedBuffer, and applies checkTimeout. It is
// the shared execution core for go build, go vet, and target-repo
// lint.
//
// The minimal env mirrors internal/implementer/tools.go:275-281 to
// prevent a compromised target repo from exfiltrating API keys.
// GOFLAGS=-mod=readonly makes `go build`/`go vet` fail instead of
// silently mutating go.mod/go.sum when a dependency is missing
// (it also applies when `make lint` or `golangci-lint` transitively
// invoke go tools). GOWORK=off disables workspace mode so validation
// stays deterministic regardless of the CI environment's workspace
// config.
//
// label is used only in error messages for timeouts and non-exit
// failures (e.g. "go build ./...", "make lint"). A deadline exceeded
// on ctx returns (nil, error). A non-zero exit code is a verdict,
// not a runner error: it produces a non-nil CheckResult with
// Passed=false.
func runBoundedCmd(ctx context.Context, dir, label, name string, args ...string) (*CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOPATH=" + os.Getenv("GOPATH"),
		"GOROOT=" + os.Getenv("GOROOT"),
		"TMPDIR=" + os.TempDir(),
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
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
		return nil, fmt.Errorf("%s timed out after %s", label, checkTimeout)
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., binary missing) — bubble up.
			return nil, fmt.Errorf("running %s: %w", label, runErr)
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
	return runBoundedCmd(ctx, repoDir, "go build ./...", "go", "build", "./...")
}

// RunVet executes `go vet ./...` in repoDir.
func RunVet(ctx context.Context, repoDir string) (*CheckResult, error) {
	return runBoundedCmd(ctx, repoDir, "go vet ./...", "go", "vet", "./...")
}
