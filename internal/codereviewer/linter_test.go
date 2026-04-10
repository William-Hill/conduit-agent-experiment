package codereviewer

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFilterLintErrors(t *testing.T) {
	cases := []struct {
		name          string
		output        string
		repoDir       string
		changedFiles  []string
		wantKept      []lintError
		wantDropped   int
		wantTruncated bool
	}{
		{
			name: "canonical golangci-lint format with mixed files",
			output: "internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
				"internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
				"internal/unchanged/baz.go:11:1: var X should be Y (stylecheck)\n",
			changedFiles: []string{"internal/foo/bar.go"},
			wantKept: []lintError{
				{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
				{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
			},
			wantDropped: 1,
		},
		{
			name:         "no column (line-only)",
			output:       "foo.go:42: some message\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 42, Col: 0, Message: "some message"},
			},
			wantDropped: 0,
		},
		{
			name:         "path normalization strips leading ./",
			output:       "./internal/foo.go:10:1: msg\n",
			changedFiles: []string{"internal/foo.go"},
			wantKept: []lintError{
				{File: "internal/foo.go", Line: 10, Col: 1, Message: "msg"},
			},
			wantDropped: 0,
		},
		{
			name: "non-matching garbage lines are ignored (not counted)",
			output: "level=warning msg=\"something unrelated\"\n" +
				"make: *** [lint] Error 1\n" +
				"foo.go:1:1: real error\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 1, Col: 1, Message: "real error"},
			},
			wantDropped: 0,
		},
		{
			name: "parser cap at 500 lines",
			output: func() string {
				var sb strings.Builder
				for i := 0; i < 1000; i++ {
					sb.WriteString("unchanged.go:1:1: msg\n")
				}
				return sb.String()
			}(),
			changedFiles:  []string{"something_else.go"},
			wantKept:      nil,
			wantDropped:   500,
			wantTruncated: true,
		},
		{
			name:         "empty output",
			output:       "",
			changedFiles: []string{"foo.go"},
			wantKept:     nil,
			wantDropped:  0,
		},
		{
			name: "absolute paths stripped against repoDir",
			output: "/work/repo/internal/foo.go:10:1: real error (ineffassign)\n" +
				"/work/repo/internal/unchanged.go:5:2: not ours\n",
			repoDir:      "/work/repo",
			changedFiles: []string{"internal/foo.go"},
			wantKept: []lintError{
				{File: "internal/foo.go", Line: 10, Col: 1, Message: "real error (ineffassign)"},
			},
			wantDropped: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped, truncated := filterLintErrors(tc.output, tc.repoDir, tc.changedFiles)
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept:\n  got:  %#v\n  want: %#v", kept, tc.wantKept)
			}
			if dropped != tc.wantDropped {
				t.Errorf("dropped: got %d, want %d", dropped, tc.wantDropped)
			}
			if truncated != tc.wantTruncated {
				t.Errorf("truncated: got %v, want %v", truncated, tc.wantTruncated)
			}
		})
	}
}

func TestFormatLintFeedback(t *testing.T) {
	errs := []lintError{
		{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
		{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
		{File: "cmd/implementer/main.go", Line: 301, Col: 12, Message: "exported function Frob should have comment (golint)"},
	}

	want := "## Lint Errors\n\n" +
		"The following lint violations were introduced by your changes. Fix each one:\n\n" +
		"- internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
		"- internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
		"- cmd/implementer/main.go:301:12: exported function Frob should have comment (golint)\n\n" +
		"Re-run the build and try again."

	got := formatLintFeedback(errs)
	if got != want {
		t.Errorf("formatLintFeedback mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestFormatLintRawFeedback(t *testing.T) {
	output := "make: *** [lint] Error 1\nsomething broke\n"
	got := formatLintRawFeedback(output)

	// Golden assertions — exact wording drift is caught here so the
	// implementer's retry prompt stays stable.
	for _, want := range []string{
		"## Lint Failure",
		"could not be cleanly matched",
		"Raw Lint Output",
		"```\nmake: *** [lint] Error 1\nsomething broke\n\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatLintRawFeedback missing %q.\nGot:\n%s", want, got)
		}
	}
}

func TestDetectLinter(t *testing.T) {
	// This test and its subtests mutate package-level state (lookPathFn
	// and the AGENT_LINT env var), so none of them may be marked
	// t.Parallel() without adding synchronization.
	// Stub lookPathFn for the whole test so we don't depend on the host
	// environment having (or not having) golangci-lint installed.
	t.Cleanup(func() { lookPathFn = exec.LookPath })

	t.Run("makefile with lint target wins", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "build:\n\tgo build ./...\n\nlint:\n\tgolangci-lint run\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != "make" {
			t.Errorf("expected Mode=make, got %+v", cfg)
		}
	})

	t.Run("makefile without lint target falls through", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "build:\n\tgo build ./...\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil config, got %+v", cfg)
		}
	})

	t.Run("golangci-lint with config on path", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "/usr/local/bin/golangci-lint", nil }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != "golangci-lint" {
			t.Errorf("expected Mode=golangci-lint, got %+v", cfg)
		}
		if !strings.HasSuffix(cfg.ConfigPath, ".golangci.yml") {
			t.Errorf("expected ConfigPath to end with .golangci.yml, got %q", cfg.ConfigPath)
		}
	})

	t.Run("golangci config but no binary returns nil", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil, got %+v", cfg)
		}
	})

	t.Run("empty repo returns nil", func(t *testing.T) {
		dir := t.TempDir()
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil, got %+v", cfg)
		}
	})

	t.Run("AGENT_LINT=off short circuits", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "Makefile"), "lint:\n\techo ok\n")
		mustWrite(t, filepath.Join(dir, ".golangci.yml"), "run:\n  timeout: 5m\n")
		lookPathFn = func(string) (string, error) { return "/usr/local/bin/golangci-lint", nil }
		t.Setenv("AGENT_LINT", "off")

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (AGENT_LINT=off), got %+v", cfg)
		}
	})

	t.Run("makefile larger than 64 KiB only scans first 64 KiB", func(t *testing.T) {
		dir := t.TempDir()
		var sb strings.Builder
		sb.WriteString("build:\n\tgo build\n\n")
		// Pad with a harmless 'other' target until we push past 64 KiB,
		// then append a lint target that lives beyond the cap.
		padding := strings.Repeat("other:\n\techo nope\n\n", 5000) // ~95 KiB
		sb.WriteString(padding)
		sb.WriteString("lint:\n\techo beyond cap\n")
		mustWrite(t, filepath.Join(dir, "Makefile"), sb.String())
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (lint target beyond 64 KiB cap), got %+v", cfg)
		}
	})

	t.Run("variable assignment is rejected (not a lint target)", func(t *testing.T) {
		dir := t.TempDir()
		// `lint:=value` is a Make variable assignment, not a target.
		// The old regex matched it as a target; the tightened regex
		// must reject it.
		mustWrite(t, filepath.Join(dir, "Makefile"), "lint:=golangci-lint run\n\nbuild:\n\tgo build ./...\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (lint:=value is a variable, not a target), got %+v", cfg)
		}
	})

	t.Run("double-colon lint rule is accepted", func(t *testing.T) {
		dir := t.TempDir()
		// GNU make supports `lint::` double-colon rules; the tightened
		// regex must still accept them.
		mustWrite(t, filepath.Join(dir, "Makefile"), "lint::\n\techo double-colon rule\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != lintModeMake {
			t.Errorf("expected Mode=make for double-colon rule, got %+v", cfg)
		}
	})

	t.Run("GNUmakefile is probed before Makefile", func(t *testing.T) {
		dir := t.TempDir()
		// GNU make resolves GNUmakefile before Makefile. The probe must
		// mirror that, so a repo with a GNUmakefile is detected.
		mustWrite(t, filepath.Join(dir, "GNUmakefile"), "lint:\n\techo gnu\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != lintModeMake {
			t.Errorf("expected Mode=make from GNUmakefile, got %+v", cfg)
		}
	})

	t.Run("lowercase makefile is probed before Makefile", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "makefile"), "lint:\n\techo lower\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg == nil || cfg.Mode != lintModeMake {
			t.Errorf("expected Mode=make from lowercase makefile, got %+v", cfg)
		}
	})

	t.Run("higher-precedence file without lint target does not fall through", func(t *testing.T) {
		dir := t.TempDir()
		// GNUmakefile exists but has no lint target; Makefile has one.
		// Make's default-file behavior is to use GNUmakefile, so we
		// must NOT fall through to Makefile — returning nil matches
		// what `make lint` would actually do (fail with "no rule to
		// make target 'lint'").
		mustWrite(t, filepath.Join(dir, "GNUmakefile"), "build:\n\techo build\n")
		mustWrite(t, filepath.Join(dir, "Makefile"), "lint:\n\techo lint\n")
		lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

		cfg, err := detectLinter(dir)
		if err != nil {
			t.Fatalf("detectLinter error: %v", err)
		}
		if cfg != nil {
			t.Errorf("expected nil (GNUmakefile wins precedence and has no lint target), got %+v", cfg)
		}
	})
}

func TestRunLint_NoConfigIsNoop(t *testing.T) {
	// Mutates package-level lookPathFn / AGENT_LINT — not parallel-safe.
	dir := t.TempDir()
	// Stub lookPathFn so no binary is considered present.
	t.Cleanup(func() { lookPathFn = exec.LookPath })
	lookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

	res, err := RunLint(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunLint error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true for no-op, got false")
	}
	if res.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "no configuration detected") {
		t.Errorf("expected no-op sentinel in output, got %q", res.Output)
	}
}

func TestRunLint_AgentLintOff(t *testing.T) {
	// Mutates package-level lookPathFn / AGENT_LINT — not parallel-safe.
	dir := t.TempDir()
	// Even with a real lint target, AGENT_LINT=off wins.
	mustWrite(t, filepath.Join(dir, "Makefile"), "lint:\n\techo should-not-run\n")
	t.Setenv("AGENT_LINT", "off")

	res, err := RunLint(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunLint error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true when disabled, got false")
	}
	if !strings.Contains(res.Output, "no configuration detected") {
		t.Errorf("expected no-op sentinel, got %q", res.Output)
	}
}
