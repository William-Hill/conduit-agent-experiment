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
	wantModel := "openrouter/qwen/qwen-2.5-coder-32b-instruct:free"
	if result.ModelName != wantModel {
		t.Errorf("ModelName = %q, want %q", result.ModelName, wantModel)
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

func TestAiderBackendRunTakesLastTokenLine(t *testing.T) {
	tempDir := t.TempDir()
	// Two Tokens: lines — first is a mid-run sub-total, second is the
	// session cumulative. We must record the second.
	fakeOut := `Applied edit.
Tokens: 500 sent, 100 received. Cost: $0.00 message, $0.00 session.
Applied edit.
Tokens: 1.2k sent, 300 received. Cost: $0.00 message, $0.00 session.
`
	fakeAider := writeFakeAider(t, tempDir, fakeOut)
	repoDir := t.TempDir()
	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", fakeAider)
	plan := &planner.ImplementationPlan{Markdown: "Do it"}
	result, err := b.Run(context.Background(), RunParams{RepoDir: repoDir, Plan: plan, MaxIterations: 10})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.InputTokens != 1200 {
		t.Errorf("InputTokens = %d, want 1200 (cumulative, not first sub-total 500)", result.InputTokens)
	}
	if result.OutputTokens != 300 {
		t.Errorf("OutputTokens = %d, want 300 (cumulative, not first sub-total 100)", result.OutputTokens)
	}
}

func TestAiderBackendRunNilPlan(t *testing.T) {
	b := NewAiderBackend("key", "openrouter/qwen/qwen-2.5-coder-32b-instruct:free", "aider")
	_, err := b.Run(context.Background(), RunParams{RepoDir: t.TempDir(), Plan: nil})
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
	if !strings.Contains(err.Error(), "nil plan") {
		t.Errorf("error should mention nil plan, got %q", err.Error())
	}
}

func TestTruncateStderrShort(t *testing.T) {
	if got := truncateStderr("short"); got != "short" {
		t.Errorf("truncateStderr should pass short strings through, got %q", got)
	}
}

func TestTruncateStderrLong(t *testing.T) {
	big := strings.Repeat("x", 8000)
	got := truncateStderr(big)
	if !strings.HasPrefix(got, "...(truncated)...") {
		t.Errorf("missing truncation marker: %q", got[:30])
	}
	// Should keep the last 4096 bytes plus the marker.
	if len(got) != len("...(truncated)...")+4096 {
		t.Errorf("truncated length = %d, want %d", len(got), len("...(truncated)...")+4096)
	}
}

func TestAiderBackendArgsContainDisablePlaywright(t *testing.T) {
	// Regression test: disable-playwright must always be set to prevent
	// aider from scraping URLs in the plan markdown and blowing past the
	// context limit. See issue #38 first-run smoke test findings.
	tempDir := t.TempDir()
	// Fake aider that echoes its argv to a file we can inspect.
	path := filepath.Join(tempDir, "aider")
	argsLog := filepath.Join(tempDir, "args.log")
	script := "#!/bin/bash\nfor a in \"$@\"; do echo \"$a\" >> " + argsLog + "; done\necho 'Tokens: 10 sent, 5 received. Cost: $0.00 message, $0.00 session.'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake aider: %v", err)
	}

	b := NewAiderBackend("key", "openrouter/x/y:free", path)
	plan := &planner.ImplementationPlan{Markdown: "Do it"}
	_, err := b.Run(context.Background(), RunParams{
		RepoDir:       t.TempDir(),
		Plan:          plan,
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	logged, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	if !strings.Contains(string(logged), "--disable-playwright") {
		t.Errorf("aider args missing --disable-playwright; got: %s", string(logged))
	}
}
