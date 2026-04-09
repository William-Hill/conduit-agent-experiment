# PR Upsert on Branch Collision — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the failing two-call `CreateBranchAndPush` + `CreateDraftPR` sequence with a single `UpsertBranchAndPR` adapter method that handles branch collisions — create new, update existing PR, suffix when prior PR was closed, or skip when prior PR was merged — so repeated scheduled runs on the same issue no longer fail.

**Architecture:** One new adapter method in `internal/github/adapter.go` with five decision branches (Create / ForcePushNewPR / Update / Suffixed / SkippedMerged), backed by small private helpers for branch existence lookups, most-recent-PR lookups, and force-pushing with lease. Both call sites (`cmd/implementer/main.go`, `internal/orchestrator/workflow.go`) migrate to the new method; the old `CreateBranchAndPush` and `CreateDraftPR` methods are deleted since they have no other consumers.

**Tech Stack:** Go 1.25, `gh` CLI via `os/exec`, standard `testing` with shell-script mocks (`writeMockScript` pattern from `adapter_test.go`), local bare git repos for push integration.

**Spec:** `docs/superpowers/specs/2026-04-09-pr-upsert-branch-collision-design.md`

---

## File Structure

**Modified:**
- `internal/github/adapter.go` — add `UpsertBranchAndPR` + private helpers, delete `CreateBranchAndPush` and `CreateDraftPR`.
- `internal/github/adapter_test.go` — remove `TestCreateBranchAndPush`, `TestCreateBranchAndPush_ForkRemote`, `TestCreateDraftPR`; add five new `TestUpsertBranchAndPR_*` tests.
- `cmd/implementer/main.go:224-243` — replace two-call sequence with single `UpsertBranchAndPR` call, handle `UpsertSkippedMerged` by logging and skipping HITL Gate 3.
- `internal/orchestrator/workflow.go:384-407` — same migration; skipped-merged case leaves `prURL` empty so evaluation bookkeeping already handles it.

No new files. Everything is scoped to the adapter and its two call sites.

---

## Task 1: Add action types and result struct

**Files:**
- Modify: `internal/github/adapter.go` (add types near the existing `DraftPRInput` type around line 40)

- [ ] **Step 1: Add `UpsertAction` and `UpsertResult` types**

Insert after the `DraftPRInput` struct:

```go
// UpsertAction describes what UpsertBranchAndPR did.
type UpsertAction string

const (
	UpsertCreated       UpsertAction = "created"        // fresh branch + new PR
	UpsertForcePushed   UpsertAction = "force_pushed"   // branch existed but no PR, force-pushed + new PR
	UpsertUpdated       UpsertAction = "updated"        // force-pushed + commented on existing open PR
	UpsertSuffixed      UpsertAction = "suffixed"       // prior PR closed, new branch with --N suffix + new PR
	UpsertSkippedMerged UpsertAction = "skipped_merged" // prior PR merged, no push, no PR
)

// UpsertResult is returned by UpsertBranchAndPR.
type UpsertResult struct {
	PRURL  string       // empty iff Action == UpsertSkippedMerged
	Branch string       // final branch name (may differ from input if suffixed)
	Action UpsertAction
}
```

- [ ] **Step 2: Verify the package still builds**

Run: `cd /Users/william-meroxa/Development/conduit-agent-experiment && go build ./internal/github/...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/github/adapter.go
git commit -m "feat(github): add UpsertAction and UpsertResult types (#30)"
```

---

## Task 2: Extract `ensureForkRemote` and `commitWorktree` helpers

**Files:**
- Modify: `internal/github/adapter.go` (factor existing logic out of `CreateBranchAndPush`)

- [ ] **Step 1: Add `ensureForkRemote` helper**

Insert as a new method on `*Adapter`, just before `CreateBranchAndPush`:

```go
// ensureForkRemote adds the "fork" git remote in worktreeDir if the fork
// differs from upstream. Idempotent — existing remotes are left alone.
// Returns the name of the push remote ("fork" if fork differs, "origin" otherwise).
func (a *Adapter) ensureForkRemote(ctx context.Context, worktreeDir string) string {
	if a.ForkOwner == "" || a.ForkOwner == a.Owner {
		return "origin"
	}
	forkURL := fmt.Sprintf("https://github.com/%s/%s.git", a.ForkOwner, a.Repo)
	addRemote := exec.CommandContext(ctx, "git", "remote", "add", "fork", forkURL)
	addRemote.Dir = worktreeDir
	addRemote.CombinedOutput() // ignore error — remote may already exist
	return "fork"
}
```

- [ ] **Step 2: Add `commitWorktree` helper**

Insert right after `ensureForkRemote`:

```go
// commitWorktree runs `git checkout -B <branch>`, `git add -A`, `git commit -m`
// in worktreeDir. Returns an error if any step fails.
func (a *Adapter) commitWorktree(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	cmds := [][]string{
		{"git", "checkout", "-B", branch},
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = worktreeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("running %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}
```

- [ ] **Step 3: Refactor `CreateBranchAndPush` to use the helpers**

Replace the body of `CreateBranchAndPush` (currently lines 120-149) with:

```go
func (a *Adapter) CreateBranchAndPush(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return err
	}
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)
	cmd := exec.CommandContext(ctx, "git", "push", "-u", pushRemote, branch)
	cmd.Dir = worktreeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("running git push -u %s %s: %w\n%s", pushRemote, branch, err, out)
	}
	return nil
}
```

- [ ] **Step 4: Run existing tests to confirm refactor didn't break anything**

Run: `go test ./internal/github/... -run TestCreateBranchAndPush -v`
Expected: PASS for `TestCreateBranchAndPush` and `TestCreateBranchAndPush_ForkRemote`.

- [ ] **Step 5: Commit**

```bash
git add internal/github/adapter.go
git commit -m "refactor(github): extract ensureForkRemote and commitWorktree helpers (#30)"
```

---

## Task 3: Add `branchExistsOnFork` helper

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestBranchExistsOnFork -v`
Expected: FAIL with "a.branchExistsOnFork undefined".

- [ ] **Step 3: Implement `branchExistsOnFork`**

Add to `adapter.go`:

```go
// branchExistsOnFork returns true if the branch exists on the fork. A 404
// from the GitHub API is interpreted as "not exists" (nil error). Any other
// error is returned as-is.
func (a *Adapter) branchExistsOnFork(ctx context.Context, branch string) (bool, error) {
	forkRepo := a.ForkOwner + "/" + a.Repo
	_, err := a.runGH(ctx, "api", fmt.Sprintf("repos/%s/branches/%s", forkRepo, branch), "--silent")
	if err == nil {
		return true, nil
	}
	// gh surfaces "HTTP 404" in stderr for not-found. runGH wraps stderr into the error string.
	if strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "Not Found") {
		return false, nil
	}
	return false, fmt.Errorf("gh api repos/%s/branches/%s: %w", forkRepo, branch, err)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestBranchExistsOnFork -v`
Expected: PASS for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): add branchExistsOnFork helper (#30)"
```

---

## Task 4: Add `mostRecentPRForBranch` helper

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestMostRecentPRForBranch -v`
Expected: FAIL with "a.mostRecentPRForBranch undefined".

- [ ] **Step 3: Implement `mostRecentPRForBranch`**

Add to `adapter.go`:

```go
// prSummary is a small PR summary used by the upsert logic.
type prSummary struct {
	Number    int    `json:"number"`
	State     string `json:"state"` // OPEN, CLOSED, MERGED
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

// mostRecentPRForBranch returns the most recent PR (by createdAt) whose head
// matches <fork-owner>:<branch>. Returns (nil, nil) when no PR exists.
func (a *Adapter) mostRecentPRForBranch(ctx context.Context, branch string) (*prSummary, error) {
	head := branch
	if a.ForkOwner != "" && a.ForkOwner != a.Owner {
		head = a.ForkOwner + ":" + branch
	}
	args := []string{
		"pr", "list",
		"--repo", a.repo(),
		"--head", head,
		"--state", "all",
		"--json", "number,state,url,createdAt",
		"--limit", "20",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr list --head %s: %w", head, err)
	}
	var prs []prSummary
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("parsing pr list output: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	// Sort descending by CreatedAt (RFC3339 lexicographic sort works).
	sort.Slice(prs, func(i, j int) bool { return prs[i].CreatedAt > prs[j].CreatedAt })
	return &prs[0], nil
}
```

- [ ] **Step 4: Add `sort` to the import block**

At the top of `adapter.go`, change:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)
```

to:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestMostRecentPRForBranch -v`
Expected: PASS for all three.

- [ ] **Step 6: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): add mostRecentPRForBranch helper (#30)"
```

---

## Task 5: Add `forcePushBranch` helper

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
// TestForcePushBranch uses two real bare repos: one acts as the fork remote,
// one acts as a "stale" clone to simulate the branch already existing on the
// fork with unrelated history.
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
```

Also add a `strings` import to the test file if not already present (it is — confirm with the top of the file).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestForcePushBranch -v`
Expected: FAIL with "a.forcePushBranch undefined".

- [ ] **Step 3: Implement `forcePushBranch`**

Add to `adapter.go`:

```go
// forcePushBranch fetches the current state of <branch> from the fork, then
// force-pushes the local HEAD to it using --force-with-lease against the
// just-fetched sha. This means we overwrite the remote branch only if it
// still matches what we just saw, protecting against races.
func (a *Adapter) forcePushBranch(ctx context.Context, worktreeDir, branch string) error {
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)

	// Fetch the branch so we have a remote-tracking ref for --force-with-lease.
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", pushRemote, branch)
	fetchCmd.Dir = worktreeDir
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch %s %s: %w\n%s", pushRemote, branch, err, out)
	}

	// Read the fetched remote sha to pin --force-with-lease.
	revCmd := exec.CommandContext(ctx, "git", "rev-parse", pushRemote+"/"+branch)
	revCmd.Dir = worktreeDir
	shaBytes, err := revCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse %s/%s: %w", pushRemote, branch, err)
	}
	expectedSha := strings.TrimSpace(string(shaBytes))

	lease := fmt.Sprintf("--force-with-lease=%s:%s", branch, expectedSha)
	pushCmd := exec.CommandContext(ctx, "git", "push", lease, pushRemote, "HEAD:"+branch)
	pushCmd.Dir = worktreeDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push --force-with-lease %s %s: %w\n%s", pushRemote, branch, err, out)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestForcePushBranch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): add forcePushBranch helper with --force-with-lease (#30)"
```

---

## Task 6: Implement `UpsertBranchAndPR` for the Create case

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_Create -v`
Expected: FAIL with "a.UpsertBranchAndPR undefined".

- [ ] **Step 3: Implement `UpsertBranchAndPR` (Create branch only)**

Add to `adapter.go`:

```go
// UpsertBranchAndPR creates or updates a branch on the fork and its draft PR,
// handling the cases where the branch or a prior PR already exists. See
// docs/superpowers/specs/2026-04-09-pr-upsert-branch-collision-design.md for
// the full decision tree.
//
// prInput.Head is ignored — the method sets it to the final branch name.
func (a *Adapter) UpsertBranchAndPR(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	return a.upsertWithDepth(ctx, worktreeDir, branch, commitMsg, prInput, 0)
}

// maxUpsertSuffixDepth bounds the suffix search when prior PRs are closed.
// -2, -3, ..., -10 → depth 9.
const maxUpsertSuffixDepth = 9

func (a *Adapter) upsertWithDepth(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
	depth int,
) (UpsertResult, error) {
	exists, err := a.branchExistsOnFork(ctx, branch)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("checking branch exists: %w", err)
	}
	if !exists {
		return a.createFresh(ctx, worktreeDir, branch, commitMsg, prInput)
	}
	// Branch exists — decide based on most recent PR.
	// (Remaining cases implemented in later tasks.)
	return UpsertResult{}, fmt.Errorf("branch %s exists but upsert decision not yet implemented", branch)
}

// createFresh commits, pushes, and creates a draft PR for a branch that does
// not already exist on the fork.
func (a *Adapter) createFresh(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	if err := a.CreateBranchAndPush(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("create branch and push: %w", err)
	}
	prInput.Head = branch
	url, err := a.CreateDraftPR(ctx, prInput)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("create draft PR: %w", err)
	}
	return UpsertResult{PRURL: url, Branch: branch, Action: UpsertCreated}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_Create -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): UpsertBranchAndPR Create case (#30)"
```

---

## Task 7: Implement the Update case (existing open PR)

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_Update -v`
Expected: FAIL with "branch … exists but upsert decision not yet implemented".

- [ ] **Step 3: Extend `upsertWithDepth` to handle OPEN state**

Replace the body of `upsertWithDepth` with:

```go
func (a *Adapter) upsertWithDepth(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
	depth int,
) (UpsertResult, error) {
	exists, err := a.branchExistsOnFork(ctx, branch)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("checking branch exists: %w", err)
	}
	if !exists {
		return a.createFresh(ctx, worktreeDir, branch, commitMsg, prInput)
	}

	pr, err := a.mostRecentPRForBranch(ctx, branch)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("looking up most recent PR: %w", err)
	}

	switch {
	case pr != nil && pr.State == "OPEN":
		return a.updateExisting(ctx, worktreeDir, branch, commitMsg, pr)
	default:
		return UpsertResult{}, fmt.Errorf("branch %s PR state %q not yet handled", branch, stateOf(pr))
	}
}

// stateOf returns a human-readable state for the pr, or "none" when pr is nil.
func stateOf(pr *prSummary) string {
	if pr == nil {
		return "none"
	}
	return pr.State
}

// updateExisting commits, force-pushes, and posts a timestamped "Updated by
// automated run" comment on the existing open PR.
func (a *Adapter) updateExisting(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	pr *prSummary,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	if err := a.forcePushBranch(ctx, worktreeDir, branch); err != nil {
		return UpsertResult{}, fmt.Errorf("force push: %w", err)
	}
	body := fmt.Sprintf("Updated by automated run at %s", time.Now().UTC().Format(time.RFC3339))
	if err := a.PostComment(ctx, pr.Number, body); err != nil {
		return UpsertResult{}, fmt.Errorf("post update comment: %w", err)
	}
	return UpsertResult{PRURL: pr.URL, Branch: branch, Action: UpsertUpdated}, nil
}
```

- [ ] **Step 4: Add `time` to the import block of `adapter.go`**

At the top, change:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)
```

to:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_Update -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): UpsertBranchAndPR Update case for open PRs (#30)"
```

---

## Task 8: Implement the SkippedMerged case

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_SkippedMerged -v`
Expected: FAIL with "PR state \"MERGED\" not yet handled".

- [ ] **Step 3: Add the MERGED branch to the switch**

In `upsertWithDepth`, replace:

```go
	switch {
	case pr != nil && pr.State == "OPEN":
		return a.updateExisting(ctx, worktreeDir, branch, commitMsg, pr)
	default:
		return UpsertResult{}, fmt.Errorf("branch %s PR state %q not yet handled", branch, stateOf(pr))
	}
```

with:

```go
	switch {
	case pr != nil && pr.State == "OPEN":
		return a.updateExisting(ctx, worktreeDir, branch, commitMsg, pr)
	case pr != nil && pr.State == "MERGED":
		log.Printf("UpsertBranchAndPR: branch %s has merged PR #%d, skipping push", branch, pr.Number)
		return UpsertResult{Branch: branch, Action: UpsertSkippedMerged}, nil
	default:
		return UpsertResult{}, fmt.Errorf("branch %s PR state %q not yet handled", branch, stateOf(pr))
	}
```

- [ ] **Step 4: Add `log` to the imports**

Change the import block to include `"log"`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_SkippedMerged -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): UpsertBranchAndPR skip path for merged PRs (#30)"
```

---

## Task 9: Implement the Suffixed case (closed PR → recurse)

**Files:**
- Modify: `internal/github/adapter.go`
- Test: `internal/github/adapter_test.go`

> **Historical note:** The code/tests below use a single-dash suffix (`agent/fix-1-2`). During review the final implementation was revised to use a **double-dash** marker (`agent/fix-1--2`) because the single-dash form produced a false-positive for low-numbered issues (e.g. `agent/fix-7` would be parsed as having suffix 7). The revision commit is `0afe14f`. Read this task as written, then treat every `-<N>` suffix literal as `--<N>` when matching against the final `internal/github/adapter.go`. The plan is preserved as a historical record of the original TDD sequence.

- [ ] **Step 1: Write the failing test**

Append to `adapter_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_SuffixedWhenClosed -v`
Expected: FAIL with "PR state \"CLOSED\" not yet handled".

- [ ] **Step 3: Add the CLOSED branch to the switch and implement recursion**

In `upsertWithDepth`, replace the `switch` with:

```go
	switch {
	case pr != nil && pr.State == "OPEN":
		return a.updateExisting(ctx, worktreeDir, branch, commitMsg, pr)
	case pr != nil && pr.State == "MERGED":
		log.Printf("UpsertBranchAndPR: branch %s has merged PR #%d, skipping push", branch, pr.Number)
		return UpsertResult{Branch: branch, Action: UpsertSkippedMerged}, nil
	case pr != nil && pr.State == "CLOSED":
		return a.recurseSuffixed(ctx, worktreeDir, branch, commitMsg, prInput, depth)
	case pr == nil:
		// Branch exists but no PR ever — treat as orphan, force-push and create.
		return a.forcePushNew(ctx, worktreeDir, branch, commitMsg, prInput)
	default:
		return UpsertResult{}, fmt.Errorf("branch %s PR state %q not recognized", branch, pr.State)
	}
```

Add these helpers below `updateExisting`:

```go
// recurseSuffixed handles the CLOSED case by recursing into the next suffix.
// It strips any existing -N suffix from the current branch name and increments.
func (a *Adapter) recurseSuffixed(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
	depth int,
) (UpsertResult, error) {
	if depth >= maxUpsertSuffixDepth {
		return UpsertResult{}, fmt.Errorf("upsert suffix cap exceeded: tried %d variations of branch %s, all had closed PRs", maxUpsertSuffixDepth+1, branch)
	}
	base, cur := parseSuffix(branch)
	next := cur + 1
	if next < 2 {
		next = 2
	}
	nextBranch := fmt.Sprintf("%s-%d", base, next)
	result, err := a.upsertWithDepth(ctx, worktreeDir, nextBranch, commitMsg, prInput, depth+1)
	if err != nil {
		return result, err
	}
	// Any terminal outcome at the suffixed name is reported as Suffixed so
	// callers can tell the suffix logic kicked in, EXCEPT skipped-merged which
	// we propagate as-is (the work is already done upstream).
	if result.Action != UpsertSkippedMerged {
		result.Action = UpsertSuffixed
	}
	return result, nil
}

// forcePushNew handles "branch exists but no PR ever" — force-push and create.
func (a *Adapter) forcePushNew(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	if err := a.forcePushBranch(ctx, worktreeDir, branch); err != nil {
		return UpsertResult{}, fmt.Errorf("force push: %w", err)
	}
	prInput.Head = branch
	url, err := a.CreateDraftPR(ctx, prInput)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("create draft PR: %w", err)
	}
	return UpsertResult{PRURL: url, Branch: branch, Action: UpsertForcePushed}, nil
}

// parseSuffix splits a branch name like "agent/fix-1" into ("agent/fix-1", 0)
// or "agent/fix-1-3" into ("agent/fix-1", 3). The "base" never ends in -N for
// N >= 2; the first call on a fresh branch returns depth 0 and produces -2.
var suffixRe = regexp.MustCompile(`^(.*)-(\d+)$`)

func parseSuffix(branch string) (base string, n int) {
	m := suffixRe.FindStringSubmatch(branch)
	if m == nil {
		return branch, 0
	}
	// Only treat trailing -N as a suffix when N >= 2; agent/fix-1268 has
	// issue number 1268, not suffix 1268.
	var parsed int
	if _, err := fmt.Sscanf(m[2], "%d", &parsed); err != nil || parsed < 2 {
		return branch, 0
	}
	return m[1], parsed
}
```

- [ ] **Step 4: Add `regexp` to the imports**

Change the imports to include `"regexp"`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_SuffixedWhenClosed -v`
Expected: PASS.

- [ ] **Step 6: Add parseSuffix unit test**

Append to `adapter_test.go`:

```go
func TestParseSuffix(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantN    int
	}{
		{"agent/fix-1", "agent/fix-1", 0},        // issue number, not suffix
		{"agent/fix-1268", "agent/fix-1268", 0},  // issue number
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
```

Run: `go test ./internal/github/... -run TestParseSuffix -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "feat(github): UpsertBranchAndPR suffix recursion for closed PRs (#30)"
```

---

## Task 10: Implement the SuffixCapExceeded error case

**Files:**
- Test: `internal/github/adapter_test.go`

No code changes — the cap is already enforced in `recurseSuffixed`. This task just verifies with a test.

- [ ] **Step 1: Write the test**

Append to `adapter_test.go`:

```go
// TestUpsertBranchAndPR_SuffixCapExceeded: every candidate from agent/fix-1
// through agent/fix-1-10 has a CLOSED PR. Expects an error mentioning the cap.
func TestUpsertBranchAndPR_SuffixCapExceeded(t *testing.T) {
	repoDir := t.TempDir() // unused for push since we never get there

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "gh")
	// Every branch lookup returns 200; every pr list returns a CLOSED PR.
	script := `#!/bin/sh
case "$*" in
  *"api repos/fk/r/branches/agent/fix-1"*)
    echo '{"name":"agent/fix-1"}'
    ;;
  *"pr list"*"--head fk:agent/fix-1"*)
    echo '[{"number":1,"state":"CLOSED","url":"https://github.com/up/r/pull/1","createdAt":"2026-04-01T10:00:00Z"}]'
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

	_, err := a.UpsertBranchAndPR(context.Background(), repoDir,
		"agent/fix-1", "won't matter",
		DraftPRInput{Title: "t", Body: "b", Base: "main"},
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cap exceeded") {
		t.Errorf("error = %v, want containing 'cap exceeded'", err)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR_SuffixCapExceeded -v`
Expected: PASS.

- [ ] **Step 3: Run the full UpsertBranchAndPR test suite**

Run: `go test ./internal/github/... -run TestUpsertBranchAndPR -v`
Expected: PASS for all five (Create, Update, SkippedMerged, SuffixedWhenClosed, SuffixCapExceeded).

- [ ] **Step 4: Commit**

```bash
git add internal/github/adapter_test.go
git commit -m "test(github): verify UpsertBranchAndPR suffix cap enforcement (#30)"
```

---

## Task 11: Migrate `cmd/implementer/main.go` to UpsertBranchAndPR

**Files:**
- Modify: `cmd/implementer/main.go:223-250` (the "10. Create branch, commit, push, draft PR" block plus the HITL guard right after)

- [ ] **Step 1: Replace the two-call sequence with a single upsert call**

Replace the block at lines 223-243 (from `// 10. Create branch, commit, push, draft PR` through `log.Fatalf("creating draft PR: %v", err)`) with:

```go
	// 10. Create or update branch and draft PR (handles collisions)
	branch := fmt.Sprintf("agent/fix-%d", issue.Number)
	commitMsg := fmt.Sprintf("fix: %s\n\nFixes #%d\n\nGenerated by conduit-agent-experiment implementer.", issue.Title, issue.Number)

	modelDisplay := modelName
	if modelDisplay == "" {
		modelDisplay = "Haiku 4.5"
	}

	prInput := github.DraftPRInput{
		Title: fmt.Sprintf("fix: %s", issue.Title),
		Body:  fmt.Sprintf("Fixes #%d\n\n## Agent Summary\n\n%s\n\n---\nGenerated by conduit-agent-experiment (archivist: Gemini Flash, implementer: %s, %d iterations).", issue.Number, result.Summary, modelDisplay, result.Iterations),
		Base:  "main",
	}

	upsert, err := adapter.UpsertBranchAndPR(ctx, repoDir, branch, commitMsg, prInput)
	if err != nil {
		log.Fatalf("upserting branch and PR: %v", err)
	}
	prURL := upsert.PRURL
	branch = upsert.Branch // may be suffixed

	switch upsert.Action {
	case github.UpsertSkippedMerged:
		log.Printf("Skipping PR: branch %s already has a merged PR upstream", branch)
	case github.UpsertUpdated:
		log.Printf("Updated existing PR: %s", prURL)
	case github.UpsertSuffixed:
		log.Printf("Created suffixed PR on %s: %s", branch, prURL)
	case github.UpsertForcePushed:
		log.Printf("Force-pushed orphan branch %s, new PR: %s", branch, prURL)
	default:
		log.Printf("Draft PR created: %s", prURL)
	}
```

- [ ] **Step 2: Remove the now-redundant prURL assignment**

The original code had:

```go
	log.Printf("Draft PR created: %s", prURL)

	// Update artifact with PR URL
	if artifactDir := os.Getenv("IMPL_ARTIFACT_DIR"); artifactDir != "" {
		appendPRURL(artifactDir, prURL)
	}
```

The `log.Printf("Draft PR created: %s", prURL)` line is now inside the switch above, so remove it from here. The artifact-update block stays but should guard against empty `prURL`:

```go
	// Update artifact with PR URL (skipped when no PR was created)
	if prURL != "" {
		if artifactDir := os.Getenv("IMPL_ARTIFACT_DIR"); artifactDir != "" {
			appendPRURL(artifactDir, prURL)
		}
	}
```

- [ ] **Step 3: Guard the HITL Gate 3 block on `prURL != ""`**

The HITL block at line 253 currently starts with:

```go
	// 11. Gate 3: Bot review loop + human approval (HITL)
	if hitlCfg.Gate3Enabled {
```

Change to:

```go
	// 11. Gate 3: Bot review loop + human approval (HITL)
	// Skipped when there's no PR (upsert skipped a merged branch).
	if hitlCfg.Gate3Enabled && prURL != "" {
```

- [ ] **Step 4: Verify the package builds**

Run: `go build ./cmd/implementer/...`
Expected: no output, exit 0.

- [ ] **Step 5: Run adapter tests to confirm no regression**

Run: `go test ./internal/github/... -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/implementer/main.go
git commit -m "feat(implementer): migrate to UpsertBranchAndPR for collision handling (#30)"
```

---

## Task 12: Migrate `internal/orchestrator/workflow.go` to UpsertBranchAndPR

**Files:**
- Modify: `internal/orchestrator/workflow.go:381-407` (the "--- 11. GitHub PR ---" block)

- [ ] **Step 1: Replace the two-call sequence**

Replace the block at lines 381-407 (from `// --- 11. GitHub PR ---` through `prURL = url` and the closing `}`) with:

```go
	// --- 11. GitHub PR ---
	prURL := ""
	if ghAdapter != nil && policy.AllowPush && (architectReview.Recommendation == agents.RecommendApprove) {
		branchName := buildBranchName(task)
		commitMsg := fmt.Sprintf("agent: %s\n\n%s", task.Title, plan.PlanSummary)

		prBody := buildPRBody(dossier, plan, verifierReport, architectReview)
		baseBranch := ghAdapter.BaseBranch
		if baseBranch == "" {
			baseBranch = "main"
		}

		upsert, err := ghAdapter.UpsertBranchAndPR(ctx, runner.WorkDir, branchName, commitMsg, github.DraftPRInput{
			Title: task.Title,
			Body:  prBody,
			Base:  baseBranch,
		})
		if err != nil {
			return nil, fmt.Errorf("upserting branch and PR: %w", err)
		}
		prURL = upsert.PRURL
		if upsert.Action == github.UpsertSkippedMerged {
			log.Printf("orchestrator: branch %s already merged upstream, skipping PR creation", branchName)
		}
	}
```

- [ ] **Step 2: Verify the orchestrator package builds**

Run: `go build ./internal/orchestrator/...`
Expected: no output, exit 0.

- [ ] **Step 3: Run orchestrator tests**

Run: `go test ./internal/orchestrator/... -v`
Expected: PASS (no tests touch the GitHub push path directly, but compilation matters).

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/workflow.go
git commit -m "feat(orchestrator): migrate to UpsertBranchAndPR for collision handling (#30)"
```

---

## Task 13: Delete `CreateBranchAndPush` and `CreateDraftPR`

Once both call sites are migrated, the legacy methods have no remaining consumers outside the adapter itself and its tests.

**Files:**
- Modify: `internal/github/adapter.go` (delete two methods)
- Modify: `internal/github/adapter_test.go` (delete three tests)
- Modify: `internal/github/adapter.go` (update `createFresh` and `forcePushNew` to inline logic instead of calling the deleted methods)

- [ ] **Step 1: Verify no remaining callers**

Run: `grep -rn 'CreateBranchAndPush\|CreateDraftPR' --include='*.go' /Users/william-meroxa/Development/conduit-agent-experiment`
Expected: results only inside `internal/github/adapter.go` (the definitions + callers we're about to inline) and `internal/github/adapter_test.go` (tests we're about to delete).

- [ ] **Step 2: Rename `CreateDraftPR` to `createDraftPR`**

Rename the method and drop the public doc comment. Change:

```go
// CreateDraftPR creates a draft PR via the gh CLI and returns the PR URL.
func (a *Adapter) CreateDraftPR(ctx context.Context, input DraftPRInput) (string, error) {
```

to:

```go
// createDraftPR creates a draft PR via the gh CLI and returns the PR URL.
func (a *Adapter) createDraftPR(ctx context.Context, input DraftPRInput) (string, error) {
```

- [ ] **Step 3: Inline the push logic into `createFresh`**

Replace `createFresh` with:

```go
func (a *Adapter) createFresh(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)
	cmd := exec.CommandContext(ctx, "git", "push", "-u", pushRemote, branch)
	cmd.Dir = worktreeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return UpsertResult{}, fmt.Errorf("git push -u %s %s: %w\n%s", pushRemote, branch, err, out)
	}
	prInput.Head = branch
	url, err := a.createDraftPR(ctx, prInput)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("create draft PR: %w", err)
	}
	return UpsertResult{PRURL: url, Branch: branch, Action: UpsertCreated}, nil
}
```

- [ ] **Step 4: Update `forcePushNew` to call `createDraftPR`**

In the existing `forcePushNew`, change `a.CreateDraftPR(ctx, prInput)` to `a.createDraftPR(ctx, prInput)`.

- [ ] **Step 5: Delete `CreateBranchAndPush`**

Remove the entire `CreateBranchAndPush` function (the refactored version from Task 2). Nothing outside the adapter still calls it, and `createFresh` now contains the push logic directly.

- [ ] **Step 6: Delete the old tests**

From `adapter_test.go`, delete:
- `TestCreateBranchAndPush`
- `TestCreateBranchAndPush_ForkRemote`
- `TestCreateDraftPR`

Their behaviors are covered by `TestUpsertBranchAndPR_Create` (for the Create case) and `TestForcePushBranch` (for force-push semantics).

- [ ] **Step 7: Build and run the full test suite for the adapter**

Run: `go build ./internal/github/... && go test ./internal/github/... -v`
Expected: all tests PASS.

- [ ] **Step 8: Build the full project to catch any missed callers**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 9: Commit**

```bash
git add internal/github/adapter.go internal/github/adapter_test.go
git commit -m "refactor(github): delete CreateBranchAndPush and CreateDraftPR (#30)"
```

---

## Task 14: Final verification

**Files:** none — pure verification.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: all tests PASS.

- [ ] **Step 2: Run `go vet`**

Run: `go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Confirm the commit history**

Run: `git log --oneline main..HEAD`
Expected: roughly 13 commits on the `feature/pr-upsert-branch-collision` branch (one per task, plus the initial spec commit from the brainstorming phase).

- [ ] **Step 4: Confirm every acceptance criterion from issue #30**

Cross-check the issue's acceptance list by hand:

- Branch collision no longer causes pipeline failure → covered by `TestUpsertBranchAndPR_Update`
- Repeated runs on the same issue update the existing PR → covered by `TestUpsertBranchAndPR_Update`
- PR comment added when updating: "Updated by automated run at <timestamp>" → asserted in `TestUpsertBranchAndPR_Update` (`strings.Contains(log, "Updated by automated run")`)
- Closed PRs trigger new branch creation with numeric suffix → covered by `TestUpsertBranchAndPR_SuffixedWhenClosed`
- Unit tests for the upsert logic → five `TestUpsertBranchAndPR_*` tests + `TestParseSuffix` + the three helper tests
- Observed in an actual scheduled run (two runs on the same issue) → post-merge validation, called out in the spec's "Validation" section

---

## Notes for the implementer

- The `gh` CLI mock pattern (shell script that routes on `$*`) is the existing idiom in `adapter_test.go`. Don't introduce an interface/mock layer for gh — the mocks are easy to write and stay close to real behavior.
- `--force-with-lease` without a value fails when there's no remote-tracking ref, which is why Task 5 explicitly fetches first and then pins the lease to the just-fetched sha.
- The suffix parser is deliberately conservative: `parseSuffix("agent/fix-1")` returns `(base="agent/fix-1", n=0)` because trailing `-1` is the issue number, not a suffix. Only `-2` and above are treated as suffixes. This means the first recursion produces `-2`, not `-1`.
- `cmd/implementer/main.go` has a third push inside the HITL bot review loop (lines 312-321). That push uses the local branch's upstream set by the first push, so it's a fast-forward and not affected by the collision bug. Leave it alone.
- If you're running tests locally and `git init -b main` isn't recognized, your system git is too old (< 2.28). CI uses Go 1.25 images which have modern git.
