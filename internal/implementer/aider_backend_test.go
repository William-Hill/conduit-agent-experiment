package implementer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

// writeFakeAider writes a bash script to tempDir that mimics aider's output
// format and returns its path. Skips on Windows.
func writeFakeAider(t *testing.T, tempDir, stdout string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake aider script is bash-only")
	}
	path := filepath.Join(tempDir, "aider")
	script := "#!/bin/bash\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aider: %v", err)
	}
	return path
}

func TestAiderBackendName(t *testing.T) {
	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", "aider")
	want := "aider:openrouter/qwen/qwen-2.5-coder-32b-instruct:free"
	if got := b.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestAiderBackendRunHappyPath(t *testing.T) {
	tempDir := t.TempDir()
	fakeOut := `Added file1.go to the chat.
Applied edit to file1.go
Commit abc1234 feat: thing
Tokens: 12.3k sent, 1.5k received. Cost: $0.00 message, $0.00 session.
`
	fakeAider := writeFakeAider(t, tempDir, fakeOut)

	// Minimal fake repo — aider is faked so no real edits happen.
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "file1.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", fakeAider)
	plan := &planner.ImplementationPlan{Markdown: "Do the thing"}
	result, err := b.Run(context.Background(), RunParams{
		RepoDir:       repoDir,
		Plan:          plan,
		TargetFiles:   []string{"file1.go"},
		MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.InputTokens != 12300 {
		t.Errorf("InputTokens = %d, want 12300", result.InputTokens)
	}
	if result.OutputTokens != 1500 {
		t.Errorf("OutputTokens = %d, want 1500", result.OutputTokens)
	}
	if result.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1 (single aider invocation)", result.Iterations)
	}
	if !strings.Contains(result.Summary, "abc1234") {
		t.Errorf("Summary should include commit sha, got %q", result.Summary)
	}
}

func TestAiderBackendRunAiderNonzeroExit(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "aider")
	// Script prints an error and exits non-zero.
	script := "#!/bin/bash\necho 'aider: rate limit exceeded' >&2\nexit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aider: %v", err)
	}

	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", path)
	plan := &planner.ImplementationPlan{Markdown: "Do the thing"}
	_, err := b.Run(context.Background(), RunParams{
		RepoDir:       t.TempDir(),
		Plan:          plan,
		MaxIterations: 10,
	})
	if err == nil {
		t.Fatal("expected error from non-zero aider exit, got nil")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should include stderr, got %q", err.Error())
	}
}

func TestParseAiderTokenLine(t *testing.T) {
	cases := []struct {
		line    string
		wantIn  int
		wantOut int
		wantOK  bool
	}{
		{"Tokens: 12.3k sent, 1.5k received. Cost: $0.00 message, $0.00 session.", 12300, 1500, true},
		{"Tokens: 500 sent, 250 received. Cost: $0.01 message, $0.02 session.", 500, 250, true},
		{"Tokens: 2.0M sent, 100k received. Cost: $0.00 message, $0.00 session.", 2_000_000, 100_000, true},
		{"no tokens line here", 0, 0, false},
	}
	for _, tc := range cases {
		in, out, ok := parseAiderTokens(tc.line)
		if ok != tc.wantOK || in != tc.wantIn || out != tc.wantOut {
			t.Errorf("parseAiderTokens(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tc.line, in, out, ok, tc.wantIn, tc.wantOut, tc.wantOK)
		}
	}
}
