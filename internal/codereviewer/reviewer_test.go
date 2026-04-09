package codereviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestModule creates a temp dir with go.mod and main.go, returning
// the directory path. Uses t.TempDir so cleanup is automatic.
func writeTestModule(t *testing.T, mainContent string) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module testmod\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "main.go"), mainContent)
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestRunBuild_Passes(t *testing.T) {
	dir := writeTestModule(t, "package main\n\nfunc main() {}\n")
	res, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed=true, got false. Output: %s", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", res.ExitCode)
	}
}

func TestRunBuild_Fails(t *testing.T) {
	// undefinedSymbol() is an unresolved reference → compile error.
	dir := writeTestModule(t, "package main\n\nfunc main() { undefinedSymbol() }\n")
	res, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if res.Passed {
		t.Error("expected Passed=false for undefined symbol")
	}
	if res.ExitCode == 0 {
		t.Error("expected non-zero ExitCode")
	}
	if !strings.Contains(res.Output, "undefined") {
		t.Errorf("expected output to contain 'undefined', got: %s", res.Output)
	}
}
