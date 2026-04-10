package codereviewer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
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

func TestRunVet_CatchesFormatMismatch(t *testing.T) {
	// fmt.Printf with wrong format verb — go build passes, go vet fails.
	content := `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`
	dir := writeTestModule(t, content)

	// Sanity: build should pass (vet is what catches this).
	build, err := RunBuild(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunBuild error: %v", err)
	}
	if !build.Passed {
		t.Fatalf("expected build to pass, got: %s", build.Output)
	}

	vet, err := RunVet(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunVet error: %v", err)
	}
	if vet.Passed {
		t.Errorf("expected vet to fail on %%d vs string mismatch. Output: %s", vet.Output)
	}
	if vet.ExitCode == 0 {
		t.Errorf("expected non-zero ExitCode")
	}
}

func TestRunBuild_Timeout(t *testing.T) {
	dir := writeTestModule(t, "package main\n\nfunc main() {}\n")
	// Deadline in the past → exec will be cancelled before it can run.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // ensure deadline has elapsed

	_, err := RunBuild(ctx, dir)
	if err == nil {
		t.Error("expected error from expired context, got nil")
	}
}

func TestRunVet_OutputTruncation(t *testing.T) {
	// Exercises the shared runGo truncation path via RunVet.
	//
	// We use RunVet rather than RunBuild because the Go compiler caps
	// error reporting after ~10 errors, making it impractical to push a
	// build-failure output past the 16 KiB maxCheckOutput threshold. vet
	// does not have the same cap, so 5000 fmt.Printf format-verb
	// mismatches reliably produce ~450 KiB of output — plenty to
	// verify truncation. Both RunBuild and RunVet delegate to runGo,
	// so the truncation code path exercised here is the same one
	// RunBuild would hit if its output were large enough.
	var sb strings.Builder
	sb.WriteString("package main\n\nimport \"fmt\"\n\nfunc main() {\n")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "\tfmt.Printf(\"%%d\", \"not a number\")  // line %d\n", i)
	}
	sb.WriteString("}\n")

	dir := writeTestModule(t, sb.String())
	res, err := RunVet(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunVet error: %v", err)
	}
	if res.Passed {
		t.Fatal("expected vet to fail")
	}
	// Allow small overhead for the "... (truncated)" suffix.
	if len(res.Output) > maxCheckOutput+64 {
		t.Errorf("output length %d exceeds cap %d", len(res.Output), maxCheckOutput+64)
	}
	if !strings.Contains(res.Output, "truncated") {
		t.Error("expected output to contain truncation marker")
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	issue := &github.Issue{
		Number: 42,
		Title:  "Fix the bug",
		Body:   "The thing is broken",
	}
	plan := &planner.ImplementationPlan{
		Markdown: "# Plan\n\nWrite code to fix main.go",
	}
	dossier := &archivist.Dossier{
		Summary:  "Bug is in main.go",
		Approach: "Fix the nil check",
		Files: []archivist.FileEntry{
			{Path: "main.go", Reason: "has the bug"},
		},
	}
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	files := []string{"main.go"}

	prompt := buildReviewPrompt(issue, plan, dossier, diff, files)

	for _, want := range []string{
		"Fix the bug",
		"The thing is broken",
		"Write code to fix main.go",
		"Bug is in main.go",
		"Fix the nil check",
		"main.go",
		"+new",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt should contain %q", want)
		}
	}
}
