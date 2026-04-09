package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runShellInDir creates an exec.Cmd for the given shell command and working directory.
func runShellInDir(command, dir string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	return cmd
}

// writeMockScript writes an executable shell script to a temp dir and returns
// the path to that dir and the script path.
func writeMockScript(t *testing.T, script string) (dir string, scriptPath string) {
	t.Helper()
	dir = t.TempDir()
	scriptPath = filepath.Join(dir, "gh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}
	return dir, scriptPath
}

func TestListIssues(t *testing.T) {
	mockOutput := `[{"number":42,"title":"Fix the bug","labels":[{"name":"bug"}],"body":"Some body","createdAt":"2024-01-01T00:00:00Z","comments":[],"assignees":[]}]`

	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:      "testowner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "forkowner",
		GHPath:     scriptPath,
	}

	issues, err := a.ListIssues(context.Background(), IssueListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListIssues() error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	if issues[0].Number != 42 {
		t.Errorf("issue number = %d, want 42", issues[0].Number)
	}
	if issues[0].Title != "Fix the bug" {
		t.Errorf("issue title = %q, want 'Fix the bug'", issues[0].Title)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "bug" {
		t.Errorf("unexpected labels: %v", issues[0].Labels)
	}
}

func TestListIssuesWithLabels(t *testing.T) {
	// Mock script that verifies --label flag is present in args
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"--label"*)
    echo '[{"number":7,"title":"Labeled issue","labels":[{"name":"enhancement"}],"body":"","createdAt":"2024-02-01T00:00:00Z","comments":[],"assignees":[]}]'
    ;;
  *)
    echo "missing --label flag" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:      "testowner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "forkowner",
		GHPath:     scriptPath,
	}

	issues, err := a.ListIssues(context.Background(), IssueListOpts{
		Limit:  5,
		Labels: []string{"enhancement"},
	})
	if err != nil {
		t.Fatalf("ListIssues() with labels error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Number != 7 {
		t.Errorf("issue number = %d, want 7", issues[0].Number)
	}
}

func TestGetIssue(t *testing.T) {
	mockOutput := `{"number":99,"title":"Single issue","labels":[],"body":"Detailed body","createdAt":"2024-03-01T00:00:00Z","comments":[],"assignees":[]}`

	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:      "testowner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "forkowner",
		GHPath:     scriptPath,
	}

	issue, err := a.GetIssue(context.Background(), 99)
	if err != nil {
		t.Fatalf("GetIssue() error: %v", err)
	}

	if issue.Number != 99 {
		t.Errorf("issue number = %d, want 99", issue.Number)
	}
	if issue.Title != "Single issue" {
		t.Errorf("issue title = %q, want 'Single issue'", issue.Title)
	}
	if issue.Body != "Detailed body" {
		t.Errorf("issue body = %q, want 'Detailed body'", issue.Body)
	}
}

func TestCreateDraftPR(t *testing.T) {
	expectedURL := "https://github.com/testowner/testrepo/pull/123"

	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+expectedURL+"'\n")

	a := &Adapter{
		Owner:      "testowner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "forkowner",
		GHPath:     scriptPath,
	}

	url, err := a.CreateDraftPR(context.Background(), DraftPRInput{
		Title: "My draft PR",
		Body:  "PR description",
		Head:  "feature-branch",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreateDraftPR() error: %v", err)
	}

	if url != expectedURL {
		t.Errorf("PR URL = %q, want %q", url, expectedURL)
	}
}

func TestCreateBranchAndPush(t *testing.T) {
	// Set up a real git repo in a temp dir to test CreateBranchAndPush
	repoDir := t.TempDir()

	gitCmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo 'hello' > file.txt",
		"git add .",
		"git commit -m 'initial commit'",
		// Create a bare remote to push to
		"git config --local receive.denyCurrentBranch ignore",
	}
	for _, c := range gitCmds {
		cmd := runShellInDir(c, repoDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %q failed: %v\n%s", c, err, out)
		}
	}

	// Set up a remote that is also local (bare repo) so push succeeds
	remoteDir := t.TempDir()
	initCmds := []string{
		"git init --bare",
	}
	for _, c := range initCmds {
		cmd := runShellInDir(c, remoteDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("remote setup %q: %v\n%s", c, err, out)
		}
	}

	addRemote := "git remote add origin " + remoteDir
	if out, err := runShellInDir(addRemote, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add remote: %v\n%s", err, out)
	}

	// Add a new file so there is something to commit
	if err := os.WriteFile(filepath.Join(repoDir, "new_file.txt"), []byte("new content\n"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	a := &Adapter{
		Owner:      "testowner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "testowner", // same as Owner so push goes to "origin"
		GHPath:     "gh",        // not used in CreateBranchAndPush
	}

	if err := a.CreateBranchAndPush(context.Background(), repoDir, "test-branch", "test commit message"); err != nil {
		t.Fatalf("CreateBranchAndPush() error: %v", err)
	}

	// Verify the branch was created
	cmd := runShellInDir("git branch --list test-branch", repoDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("checking branch: %v", err)
	}
	if len(out) == 0 {
		t.Error("branch 'test-branch' was not created")
	}
}

func TestAddLabel(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue edit"*"--add-label"*"agent:candidate"*)
    echo ""
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.AddLabel(context.Background(), 42, "agent:candidate"); err != nil {
		t.Fatalf("AddLabel() error: %v", err)
	}
}

func TestRemoveLabel(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue edit"*"--remove-label"*"agent:candidate"*)
    echo ""
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.RemoveLabel(context.Background(), 42, "agent:candidate"); err != nil {
		t.Fatalf("RemoveLabel() error: %v", err)
	}
}

func TestGetLabels(t *testing.T) {
	mockOutput := `[{"name":"bug"},{"name":"agent:candidate"}]`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	labels, err := a.GetLabels(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetLabels() error: %v", err)
	}

	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels[1] != "agent:candidate" {
		t.Errorf("labels[1] = %q, want %q", labels[1], "agent:candidate")
	}
}

func TestPostComment(t *testing.T) {
	script := `#!/bin/sh
args="$*"
case "$args" in
  *"issue comment"*"--body"*)
    echo "https://github.com/testowner/testrepo/issues/42#issuecomment-123"
    ;;
  *)
    echo "unexpected args: $args" >&2
    exit 1
    ;;
esac
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.PostComment(context.Background(), 42, "Hello world"); err != nil {
		t.Fatalf("PostComment() error: %v", err)
	}
}

func TestGetPRState(t *testing.T) {
	mockOutput := `{"state":"OPEN","isDraft":true,"reviewDecision":"REVIEW_REQUIRED"}`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\necho '"+mockOutput+"'\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	state, err := a.GetPRState(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetPRState() error: %v", err)
	}

	if state.State != "OPEN" {
		t.Errorf("State = %q, want %q", state.State, "OPEN")
	}
	if !state.IsDraft {
		t.Error("IsDraft should be true")
	}
	if state.ReviewDecision != "REVIEW_REQUIRED" {
		t.Errorf("ReviewDecision = %q, want %q", state.ReviewDecision, "REVIEW_REQUIRED")
	}
}

func TestGetReviewThreads(t *testing.T) {
	mockOutput := `{
		"data": {
			"repository": {
				"pullRequest": {
					"reviewThreads": {
						"nodes": [
							{"id": "RT_1", "isResolved": false, "comments": {"nodes": [{"body": "Fix this"}]}},
							{"id": "RT_2", "isResolved": true, "comments": {"nodes": [{"body": "Already fixed"}]}}
						]
					}
				}
			}
		}
	}`
	_, scriptPath := writeMockScript(t, "#!/bin/sh\ncat <<'ENDOFOUTPUT'\n"+mockOutput+"\nENDOFOUTPUT\n")

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	threads, err := a.GetReviewThreads(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetReviewThreads() error: %v", err)
	}

	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads))
	}
	if threads[0].ID != "RT_1" {
		t.Errorf("threads[0].ID = %q, want %q", threads[0].ID, "RT_1")
	}
	if threads[0].IsResolved {
		t.Error("threads[0] should not be resolved")
	}
	if !threads[1].IsResolved {
		t.Error("threads[1] should be resolved")
	}
}

func TestResolveThread(t *testing.T) {
	script := `#!/bin/sh
echo '{"data":{"resolveReviewThread":{"thread":{"id":"RT_1"}}}}'
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{
		Owner:  "testowner",
		Repo:   "testrepo",
		GHPath: scriptPath,
	}

	if err := a.ResolveThread(context.Background(), "RT_1"); err != nil {
		t.Fatalf("ResolveThread() error: %v", err)
	}
}

func TestBranchExistsOnFork_True(t *testing.T) {
	// gh api returns a JSON body on 200
	_, scriptPath := writeMockScript(t, `#!/bin/sh
echo '{"name":"agent/fix-1","commit":{"sha":"abc123"}}'
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	exists, err := a.branchExistsOnFork(context.Background(), "agent/fix-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}
}

func TestBranchExistsOnFork_False(t *testing.T) {
	// gh api with HTTP 404 exits non-zero with "HTTP 404" in stderr
	_, scriptPath := writeMockScript(t, `#!/bin/sh
echo 'gh: Not Found (HTTP 404)' >&2
exit 1
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	exists, err := a.branchExistsOnFork(context.Background(), "agent/fix-nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected exists=false for 404")
	}
}

func TestBranchExistsOnFork_OtherError(t *testing.T) {
	// Non-404 errors should propagate
	_, scriptPath := writeMockScript(t, `#!/bin/sh
echo 'gh: authentication failed' >&2
exit 2
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	_, err := a.branchExistsOnFork(context.Background(), "agent/fix-1")
	if err == nil {
		t.Fatal("expected error for non-404 failure")
	}
}

func TestMostRecentPRForBranch_Open(t *testing.T) {
	// Return a list with one open PR
	_, scriptPath := writeMockScript(t, `#!/bin/sh
cat <<'EOF'
[{"number":42,"state":"OPEN","url":"https://github.com/up/r/pull/42","createdAt":"2026-04-09T10:00:00Z"}]
EOF
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	pr, err := a.mostRecentPRForBranch(context.Background(), "agent/fix-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil {
		t.Fatal("expected a PR, got nil")
	}
	if pr.Number != 42 || pr.State != "OPEN" {
		t.Errorf("PR = %+v, want Number=42 State=OPEN", pr)
	}
}

func TestMostRecentPRForBranch_Empty(t *testing.T) {
	_, scriptPath := writeMockScript(t, `#!/bin/sh
echo '[]'
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	pr, err := a.mostRecentPRForBranch(context.Background(), "agent/fix-nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil PR, got %+v", pr)
	}
}

func TestMostRecentPRForBranch_SortsDescending(t *testing.T) {
	// Two PRs: older closed, newer merged. Newer should win.
	_, scriptPath := writeMockScript(t, `#!/bin/sh
cat <<'EOF'
[
  {"number":10,"state":"CLOSED","url":"https://github.com/up/r/pull/10","createdAt":"2026-04-01T10:00:00Z"},
  {"number":20,"state":"MERGED","url":"https://github.com/up/r/pull/20","createdAt":"2026-04-05T10:00:00Z"}
]
EOF
`)
	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: scriptPath}
	pr, err := a.mostRecentPRForBranch(context.Background(), "agent/fix-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 20 || pr.State != "MERGED" {
		t.Errorf("PR = %+v, want Number=20 State=MERGED", pr)
	}
}

func TestMostRecentPRForBranch_NoFork(t *testing.T) {
	// When ForkOwner is empty, head should be the bare branch name (no "owner:" prefix).
	// Verify by capturing argv and asserting the --head value.
	argsLogPath := filepath.Join(t.TempDir(), "args.log")
	script := `#!/bin/sh
echo "$*" > ` + argsLogPath + `
echo '[{"number":7,"state":"OPEN","url":"https://github.com/up/r/pull/7","createdAt":"2026-04-09T10:00:00Z"}]'
`
	_, scriptPath := writeMockScript(t, script)

	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "", GHPath: scriptPath}
	pr, err := a.mostRecentPRForBranch(context.Background(), "my-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || pr.Number != 7 {
		t.Fatalf("unexpected pr: %+v", pr)
	}

	logged, err := os.ReadFile(argsLogPath)
	if err != nil {
		t.Fatalf("reading args log: %v", err)
	}
	loggedStr := string(logged)
	if !strings.Contains(loggedStr, "--head my-branch") {
		t.Errorf("expected --head with bare branch name, got: %s", loggedStr)
	}
	if strings.Contains(loggedStr, ":my-branch") {
		t.Errorf("should not have owner: prefix when ForkOwner is empty, got: %s", loggedStr)
	}
}

func TestCreateBranchAndPush_ForkRemote(t *testing.T) {
	// Set up a real git repo in a temp dir
	repoDir := t.TempDir()

	gitCmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"echo 'hello' > file.txt",
		"git add .",
		"git commit -m 'initial commit'",
	}
	for _, c := range gitCmds {
		cmd := runShellInDir(c, repoDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %q failed: %v\n%s", c, err, out)
		}
	}

	// Set up a bare remote to act as the fork
	forkDir := t.TempDir()
	if out, err := runShellInDir("git init --bare", forkDir).CombinedOutput(); err != nil {
		t.Fatalf("fork setup: %v\n%s", err, out)
	}

	// Add a new file so there is something to commit
	if err := os.WriteFile(filepath.Join(repoDir, "fork_file.txt"), []byte("fork content\n"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	// Manually add the fork remote pointing to our bare repo so push succeeds
	addForkCmd := "git remote add fork " + forkDir
	if out, err := runShellInDir(addForkCmd, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add fork remote: %v\n%s", err, out)
	}

	a := &Adapter{
		Owner:      "upstream-owner",
		Repo:       "testrepo",
		BaseBranch: "main",
		ForkOwner:  "fork-owner", // differs from Owner, triggers fork logic
		GHPath:     "gh",
	}

	if err := a.CreateBranchAndPush(context.Background(), repoDir, "fork-branch", "test fork commit"); err != nil {
		t.Fatalf("CreateBranchAndPush() with fork error: %v", err)
	}

	// Verify the branch was created locally
	cmd := runShellInDir("git branch --list fork-branch", repoDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("checking branch: %v", err)
	}
	if len(out) == 0 {
		t.Error("branch 'fork-branch' was not created")
	}
}

// TestForcePushBranch uses two real bare repos: one acts as the fork remote,
// one acts as a "stale" clone to simulate the branch already existing on the
// fork with unrelated history.
// TestUpsertBranchAndPR_Create: branch doesn't exist on fork, no prior PR.
// Expects the fresh-create path: commit, push, pr create.
func TestUpsertBranchAndPR_Create(t *testing.T) {
	// Set up a worktree + bare fork remote so the push path works.
	forkDir := t.TempDir()
	if out, err := runShellInDir("git init --bare", forkDir).CombinedOutput(); err != nil {
		t.Fatalf("fork init: %v\n%s", err, out)
	}

	repoDir := t.TempDir()
	setup := []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo hello > file.txt",
		"git add .",
		"git commit -m initial",
		"echo new > new.txt",
	}
	for _, c := range setup {
		if out, err := runShellInDir(c, repoDir).CombinedOutput(); err != nil {
			t.Fatalf("setup %q: %v\n%s", c, err, out)
		}
	}
	if out, err := runShellInDir("git remote add fork "+forkDir, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add fork remote: %v\n%s", err, out)
	}

	// gh mock: 404 for branch lookup, [] for pr list, URL for pr create.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "gh")
	script := `#!/bin/sh
case "$*" in
  *"api repos/fk/r/branches/agent/fix-1"*)
    echo 'gh: Not Found (HTTP 404)' >&2
    exit 1
    ;;
  *"pr list"*"--head fk:agent/fix-1"*)
    echo '[]'
    ;;
  *"pr create"*)
    echo 'https://github.com/up/r/pull/99'
    ;;
  *)
    echo "unexpected gh args: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	a := &Adapter{
		Owner: "up", Repo: "r", BaseBranch: "main", ForkOwner: "fk",
		GHPath: scriptPath,
	}

	result, err := a.UpsertBranchAndPR(context.Background(), repoDir,
		"agent/fix-1", "test commit",
		DraftPRInput{Title: "t", Body: "b", Base: "main"},
	)
	if err != nil {
		t.Fatalf("UpsertBranchAndPR() error: %v", err)
	}
	if result.Action != UpsertCreated {
		t.Errorf("Action = %q, want %q", result.Action, UpsertCreated)
	}
	if result.Branch != "agent/fix-1" {
		t.Errorf("Branch = %q, want agent/fix-1", result.Branch)
	}
	if result.PRURL != "https://github.com/up/r/pull/99" {
		t.Errorf("PRURL = %q, want https://github.com/up/r/pull/99", result.PRURL)
	}
}

// TestUpsertBranchAndPR_Update: branch exists, open PR exists.
// Expects: no pr create; force-push succeeds; comment posted on existing PR.
func TestUpsertBranchAndPR_Update(t *testing.T) {
	// Bare "fork" remote with a stale branch
	forkDir := t.TempDir()
	if out, err := runShellInDir("git init --bare", forkDir).CombinedOutput(); err != nil {
		t.Fatalf("fork init: %v\n%s", err, out)
	}
	seedDir := t.TempDir()
	for _, c := range []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo seed > s.txt",
		"git add .",
		"git commit -m seed",
		"git checkout -b agent/fix-1",
		"git remote add fork " + forkDir,
		"git push fork agent/fix-1",
	} {
		if out, err := runShellInDir(c, seedDir).CombinedOutput(); err != nil {
			t.Fatalf("seed %q: %v\n%s", c, err, out)
		}
	}

	// Worktree with unrelated history
	repoDir := t.TempDir()
	for _, c := range []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo hello > f.txt",
		"git add .",
		"git commit -m initial",
		"echo new > new.txt",
	} {
		if out, err := runShellInDir(c, repoDir).CombinedOutput(); err != nil {
			t.Fatalf("work %q: %v\n%s", c, err, out)
		}
	}
	if out, err := runShellInDir("git remote add fork "+forkDir, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add fork remote: %v\n%s", err, out)
	}

	// gh mock: branch exists (200), PR list returns one OPEN PR, comment succeeds.
	commentLogPath := filepath.Join(t.TempDir(), "comments.log")
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "gh")
	script := `#!/bin/sh
case "$*" in
  *"api repos/fk/r/branches/agent/fix-1"*)
    echo '{"name":"agent/fix-1"}'
    ;;
  *"pr list"*"--head fk:agent/fix-1"*)
    echo '[{"number":42,"state":"OPEN","url":"https://github.com/up/r/pull/42","createdAt":"2026-04-09T10:00:00Z"}]'
    ;;
  *"issue comment"*"42"*)
    echo "$*" >> ` + commentLogPath + `
    echo "commented"
    ;;
  *"pr create"*)
    echo "pr create should not be called" >&2
    exit 3
    ;;
  *)
    echo "unexpected gh args: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	a := &Adapter{
		Owner: "up", Repo: "r", BaseBranch: "main", ForkOwner: "fk",
		GHPath: scriptPath,
	}

	result, err := a.UpsertBranchAndPR(context.Background(), repoDir,
		"agent/fix-1", "updated commit",
		DraftPRInput{Title: "t", Body: "b", Base: "main"},
	)
	if err != nil {
		t.Fatalf("UpsertBranchAndPR() error: %v", err)
	}
	if result.Action != UpsertUpdated {
		t.Errorf("Action = %q, want %q", result.Action, UpsertUpdated)
	}
	if result.PRURL != "https://github.com/up/r/pull/42" {
		t.Errorf("PRURL = %q, want https://github.com/up/r/pull/42", result.PRURL)
	}

	// Confirm a comment was recorded
	logBytes, err := os.ReadFile(commentLogPath)
	if err != nil || len(logBytes) == 0 {
		t.Errorf("expected a comment to be posted, log empty or missing: %v", err)
	}
	if !strings.Contains(string(logBytes), "Updated by automated run") {
		t.Errorf("comment body missing expected prefix, got: %s", string(logBytes))
	}
}

// TestUpsertBranchAndPR_SkippedMerged: branch exists, most recent PR is MERGED.
// Expects: no push, no pr create, no comment. Returns UpsertSkippedMerged.
func TestUpsertBranchAndPR_SkippedMerged(t *testing.T) {
	// No git setup needed — we should never push
	repoDir := t.TempDir()

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "gh")
	script := `#!/bin/sh
case "$*" in
  *"api repos/fk/r/branches/agent/fix-1"*)
    echo '{"name":"agent/fix-1"}'
    ;;
  *"pr list"*"--head fk:agent/fix-1"*)
    echo '[{"number":42,"state":"MERGED","url":"https://github.com/up/r/pull/42","createdAt":"2026-04-09T10:00:00Z"}]'
    ;;
  *)
    echo "unexpected gh args: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	a := &Adapter{
		Owner: "up", Repo: "r", BaseBranch: "main", ForkOwner: "fk",
		GHPath: scriptPath,
	}

	result, err := a.UpsertBranchAndPR(context.Background(), repoDir,
		"agent/fix-1", "won't run",
		DraftPRInput{Title: "t", Body: "b", Base: "main"},
	)
	if err != nil {
		t.Fatalf("UpsertBranchAndPR() unexpected error: %v", err)
	}
	if result.Action != UpsertSkippedMerged {
		t.Errorf("Action = %q, want %q", result.Action, UpsertSkippedMerged)
	}
	if result.PRURL != "" {
		t.Errorf("PRURL = %q, want empty", result.PRURL)
	}
	if result.Branch != "agent/fix-1" {
		t.Errorf("Branch = %q, want agent/fix-1", result.Branch)
	}
}

func TestForcePushBranch(t *testing.T) {
	// Bare "fork" remote
	forkDir := t.TempDir()
	if out, err := runShellInDir("git init --bare", forkDir).CombinedOutput(); err != nil {
		t.Fatalf("fork init: %v\n%s", err, out)
	}

	// Seed the fork with a stale agent/fix-1 branch by pushing from a scratch repo
	seedDir := t.TempDir()
	seedCmds := []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo seed > f.txt",
		"git add .",
		"git commit -m seed",
		"git checkout -b agent/fix-1",
		"git remote add fork " + forkDir,
		"git push fork agent/fix-1",
	}
	for _, c := range seedCmds {
		if out, err := runShellInDir(c, seedDir).CombinedOutput(); err != nil {
			t.Fatalf("seed %q: %v\n%s", c, err, out)
		}
	}

	// Our "worktree" is a fresh repo with unrelated history
	repoDir := t.TempDir()
	workCmds := []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo hello > file.txt",
		"git add .",
		"git commit -m initial",
		"git checkout -b agent/fix-1",
		"echo new > new.txt",
		"git add .",
		"git commit -m new-work",
	}
	for _, c := range workCmds {
		if out, err := runShellInDir(c, repoDir).CombinedOutput(); err != nil {
			t.Fatalf("work %q: %v\n%s", c, err, out)
		}
	}
	if out, err := runShellInDir("git remote add fork "+forkDir, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add fork remote: %v\n%s", err, out)
	}

	a := &Adapter{Owner: "up", Repo: "r", ForkOwner: "fk", GHPath: "gh"}
	if err := a.forcePushBranch(context.Background(), repoDir, "agent/fix-1"); err != nil {
		t.Fatalf("forcePushBranch() error: %v", err)
	}

	// Confirm fork's agent/fix-1 now points at our local HEAD
	localSha, err := runShellInDir("git rev-parse HEAD", repoDir).Output()
	if err != nil {
		t.Fatalf("local rev-parse: %v", err)
	}
	forkSha, err := runShellInDir("git rev-parse agent/fix-1", forkDir).Output()
	if err != nil {
		t.Fatalf("fork rev-parse: %v", err)
	}
	if strings.TrimSpace(string(localSha)) != strings.TrimSpace(string(forkSha)) {
		t.Errorf("force push didn't update remote: local=%s fork=%s", localSha, forkSha)
	}
}

// TestUpsertBranchAndPR_SuffixedWhenClosed: the base branch has a CLOSED PR.
// Expects: recurse to agent/fix-1-2, which is fresh. Final branch is agent/fix-1-2.
func TestUpsertBranchAndPR_SuffixedWhenClosed(t *testing.T) {
	forkDir := t.TempDir()
	if out, err := runShellInDir("git init --bare", forkDir).CombinedOutput(); err != nil {
		t.Fatalf("fork init: %v\n%s", err, out)
	}

	repoDir := t.TempDir()
	for _, c := range []string{
		"git init -b main",
		"git config user.email t@t.com",
		"git config user.name t",
		"echo hello > f.txt",
		"git add .",
		"git commit -m initial",
		"echo new > new.txt",
	} {
		if out, err := runShellInDir(c, repoDir).CombinedOutput(); err != nil {
			t.Fatalf("setup %q: %v\n%s", c, err, out)
		}
	}
	if out, err := runShellInDir("git remote add fork "+forkDir, repoDir).CombinedOutput(); err != nil {
		t.Fatalf("add fork remote: %v\n%s", err, out)
	}

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "gh")
	// Ordering matters: more specific patterns (with -2 suffix) must come
	// before the base branch pattern, because shell case uses first-match.
	script := `#!/bin/sh
case "$*" in
  *"api repos/fk/r/branches/agent/fix-1-2"*)
    echo 'gh: Not Found (HTTP 404)' >&2
    exit 1
    ;;
  *"pr list"*"--head fk:agent/fix-1-2"*)
    echo '[]'
    ;;
  *"api repos/fk/r/branches/agent/fix-1"*)
    echo '{"name":"agent/fix-1"}'
    ;;
  *"pr list"*"--head fk:agent/fix-1"*)
    echo '[{"number":10,"state":"CLOSED","url":"https://github.com/up/r/pull/10","createdAt":"2026-04-01T10:00:00Z"}]'
    ;;
  *"pr create"*)
    echo 'https://github.com/up/r/pull/101'
    ;;
  *)
    echo "unexpected gh args: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	a := &Adapter{
		Owner: "up", Repo: "r", BaseBranch: "main", ForkOwner: "fk",
		GHPath: scriptPath,
	}

	result, err := a.UpsertBranchAndPR(context.Background(), repoDir,
		"agent/fix-1", "suffixed commit",
		DraftPRInput{Title: "t", Body: "b", Base: "main"},
	)
	if err != nil {
		t.Fatalf("UpsertBranchAndPR() error: %v", err)
	}
	if result.Action != UpsertSuffixed {
		t.Errorf("Action = %q, want %q", result.Action, UpsertSuffixed)
	}
	if result.Branch != "agent/fix-1-2" {
		t.Errorf("Branch = %q, want agent/fix-1-2", result.Branch)
	}
	if result.PRURL != "https://github.com/up/r/pull/101" {
		t.Errorf("PRURL = %q, want https://github.com/up/r/pull/101", result.PRURL)
	}
}

func TestParseSuffix(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantN    int
	}{
		{"agent/fix-1", "agent/fix-1", 0},       // issue number, not suffix
		{"agent/fix-1268", "agent/fix-1268", 0}, // issue number
		{"agent/fix-1-2", "agent/fix-1", 2},
		{"agent/fix-1268-3", "agent/fix-1268", 3},
		{"agent/task-xyz-slug", "agent/task-xyz-slug", 0},
	}
	for _, c := range cases {
		base, n := parseSuffix(c.in)
		if base != c.wantBase || n != c.wantN {
			t.Errorf("parseSuffix(%q) = (%q, %d), want (%q, %d)", c.in, base, n, c.wantBase, c.wantN)
		}
	}
}
