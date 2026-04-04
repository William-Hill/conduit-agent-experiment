package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
		cmd := runShell(c, repoDir)
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
		cmd := runShell(c, remoteDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("remote setup %q: %v\n%s", c, err, out)
		}
	}

	addRemote := "git remote add origin " + remoteDir
	if out, err := runShell(addRemote, repoDir).CombinedOutput(); err != nil {
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
		ForkOwner:  "forkowner",
		GHPath:     "gh", // not used in CreateBranchAndPush
	}

	if err := a.CreateBranchAndPush(context.Background(), repoDir, "test-branch", "test commit message"); err != nil {
		t.Fatalf("CreateBranchAndPush() error: %v", err)
	}

	// Verify the branch was created
	cmd := runShell("git branch --list test-branch", repoDir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("checking branch: %v", err)
	}
	if len(out) == 0 {
		t.Error("branch 'test-branch' was not created")
	}
}
