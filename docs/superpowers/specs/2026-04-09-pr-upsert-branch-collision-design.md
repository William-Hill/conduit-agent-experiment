# Upsert Branch and PR on Collision

**Issue:** #30
**Date:** 2026-04-09

## Problem

When the pipeline runs on the same issue twice (e.g., the scheduled runs at
9am/3pm/9pm UTC hit the same top-ranked issue), the second run fails:

```text
creating branch and pushing: running git push -u fork agent/fix-1268: exit status 128
```

Every clone is fresh, so the local branch shares no history with the existing
`agent/fix-1268` on the fork. A plain `git push -u fork <branch>` can't
fast-forward, and the run aborts after the LLM work already completed — wasting
~$0.30 per failed run.

The failing path is `cmd/implementer/main.go:224-243`, invoked by
`.github/workflows/implement.yml`. The orchestrator path in
`internal/orchestrator/workflow.go:384-407` has the same bug using a different
branch format (`agent/task-<N>-<slug>`) and must be fixed together to keep the
adapter as the single source of truth.

## Goals

- A repeated pipeline run against the same issue never fails due to branch
  collision.
- When an open PR already exists for the target branch, the run updates it
  instead of creating a second PR.
- When the prior PR was closed (unmerged), the run creates a new branch under
  an incrementing suffix (`-2`, `-3`, …).
- When the prior PR was merged, the run skips the push and returns cleanly —
  no duplicate PR, no error.
- Single adapter entry point used by both `cmd/implementer` and
  `internal/orchestrator/workflow.go`.

## Non-goals

- Rewriting PR bodies on update (only a lightweight comment is posted — see
  decision tree below).
- Fetching the full history of every candidate branch (we only need ref
  existence and the single most recent PR).
- Integration testing the full two-runs-same-issue scenario inside this PR —
  that validation happens post-merge against the live scheduled run, and is
  listed under "Validation" below.

## Decision tree

The adapter looks up two facts for the target branch:

1. Does the branch exist on the fork? (`gh api repos/<fork>/<repo>/branches/<branch>`)
2. What is the most recent PR whose head is `<fork-owner>:<branch>`?
   (`gh pr list --head <fork-owner>:<branch> --state all --json number,state,url,createdAt --limit 20`, sorted client-side by `createdAt` descending)

From those two facts it picks one of five actions:

| State                                     | Action                | Behavior                                                                                                                             |
|-------------------------------------------|-----------------------|--------------------------------------------------------------------------------------------------------------------------------------|
| Branch does not exist                     | **Create** (`UpsertCreated`)               | `git push -u fork <branch>` → `gh pr create --draft`. Return the new PR URL.                                                                                   |
| Branch exists, no PR ever                 | **Force-push + new PR** (`UpsertForcePushed`) | `git fetch fork <branch>` → `git push --force-with-lease=<branch>:<fetched-sha> fork HEAD:<branch>` → `gh pr create --draft`.                                  |
| Branch exists, most-recent PR is **OPEN** | **Update** (`UpsertUpdated`)               | Force-push as above → `gh issue comment <prNum> --body "Updated by automated run at <RFC3339 timestamp>"`. Return the existing PR URL.                          |
| Branch exists, most-recent PR is **CLOSED** (unmerged) | **Suffixed** (`UpsertSuffixed`)         | Iterate suffix candidates `<branch>--2`, `<branch>--3`, …, `<branch>--10` (double-dash marker so issue-number branches like `agent/fix-1268` are never misread as suffixed). The first candidate whose decision is *not* "CLOSED" terminates the loop. Fail loudly if all 9 candidates also have closed PRs. |
| Branch exists, most-recent PR is **MERGED** | **Skip** (`UpsertSkippedMerged`)        | Log "upstream branch already merged, skipping push" and return `UpsertSkippedMerged`. No push, no PR creation. Caller treats as no-op.                          |

Notes:

- "Most recent PR" uses `createdAt` desc so a reopened-then-reclosed PR is
  handled the same as a closed PR.
- The suffix retry is iterative (a `for` loop), not recursive. Each candidate
  runs the full `upsertOnce` decision; closed candidates fall through to the
  next `--N`.
- The double-dash suffix marker (`--N`) is structurally unambiguous — normal
  branch names never contain `--`, so issue numbers embedded in branch names
  (e.g. `agent/fix-7`, `agent/fix-1268`) are never mistaken for a suffix.
- The suffix cap of 10 is arbitrary but generous — in practice we expect 2-3
  attempts max before a human intervenes.

## API surface

New types and method in `internal/github/adapter.go`:

```go
// UpsertAction describes what UpsertBranchAndPR did. Five variants, one
// per decision branch:
type UpsertAction string

const (
    UpsertCreated       UpsertAction = "created"        // fresh branch + new PR
    UpsertForcePushed   UpsertAction = "force_pushed"   // orphan branch (exists but no PR), force-pushed + new PR
    UpsertUpdated       UpsertAction = "updated"        // force-pushed, commented on existing open PR
    UpsertSuffixed      UpsertAction = "suffixed"       // prior PR closed, new branch --N + new PR
    UpsertSkippedMerged UpsertAction = "skipped_merged" // prior PR merged, no push
)

// UpsertResult is returned by UpsertBranchAndPR.
type UpsertResult struct {
    PRURL  string       // empty iff Action == UpsertSkippedMerged
    Branch string       // final branch name (may differ from input if suffixed)
    Action UpsertAction
}

// UpsertBranchAndPR creates or updates a branch on the fork and its draft PR,
// handling the cases where the branch or a prior PR already exists. See the
// design doc at docs/superpowers/specs/2026-04-09-pr-upsert-branch-collision-design.md
// for the full decision tree.
//
// prInput.Head is ignored; the method sets it to the final branch name.
func (a *Adapter) UpsertBranchAndPR(
    ctx context.Context,
    worktreeDir string,
    branch string,
    commitMsg string,
    prInput DraftPRInput,
) (UpsertResult, error)
```

Supporting private helpers:

- `ensureForkRemote(ctx, worktreeDir) string` — idempotent `git remote add fork …`. Returns the name of the push remote (`"fork"` if `ForkOwner` differs from upstream, otherwise `"origin"`). Shared between the first-push and force-push paths.
- `commitWorktree(ctx, worktreeDir, branch, commitMsg) error` — does `git checkout -B`, `git add -A`, `git commit -m`. Shared between the fresh-create, force-push, and update paths.
- `forkRepo() string` — returns `"<ForkOwner>/<Repo>"`, falling back to `"<Owner>/<Repo>"` when `ForkOwner` is empty (direct-push / same-repo mode).
- `branchExistsOnFork(ctx, branch) (bool, error)` — `gh api repos/<forkRepo>/branches/<branch> --silent`. Treats HTTP 404 as `(false, nil)`, any other error as real.
- `prSummary` type and `mostRecentPRForBranch(ctx, branch) (*prSummary, error)` — wraps `gh pr list --head <fork-owner>:<branch> --state all --json number,state,url,createdAt --limit 20`. Returns `nil, nil` when the list is empty. Sorts client-side by `createdAt` descending.
- `forcePushBranch(ctx, worktreeDir, branch) error` — fetches then force-pushes with `--force-with-lease=<branch>:<fetched-sha>` pinned to the just-fetched sha.
- `upsertOnce(ctx, worktreeDir, branch, commitMsg, prInput) (UpsertResult, error)` — attempts to upsert a single branch without suffix retry. Returns an internal sentinel error (`errClosedPRRetry`) when the branch has a closed PR; the caller (`UpsertBranchAndPR`) then iterates through the `--N` suffix candidates.
- `parseSuffix(branch) (base string, n int)` — splits a branch like `agent/fix-1--3` into `("agent/fix-1", 3)`. Non-suffixed branches (including issue-number-ending names like `agent/fix-1268`) return `(branch, 0)`.

### Existing methods

`CreateBranchAndPush` and `CreateDraftPR` are **removed** once both call sites
migrate. The adapter has no external consumers (verified via grep: only
`cmd/implementer/main.go`, `internal/orchestrator/workflow.go`, and the
adapter's own tests reference them). Keeping them would double the adapter
surface for no gain. The tests for those methods become tests for the
equivalent branches of `UpsertBranchAndPR`.

## Call-site changes

### `cmd/implementer/main.go`

Replace lines 224-243 (the two-call `CreateBranchAndPush` + `CreateDraftPR`
sequence) with a single `UpsertBranchAndPR` call. On
`UpsertSkippedMerged`, log `"PR for agent/fix-<N> was already merged upstream, skipping push"`
and continue to artifact/evaluation emission — no `log.Fatalf`. The Gate 3
HITL block that follows must guard on `result.PRURL != ""` so it skips cleanly
when there's no PR to review.

### `internal/orchestrator/workflow.go`

Replace lines 384-407 similarly. On `UpsertSkippedMerged`, the `prURL` stays
empty and the subsequent evaluation/run bookkeeping already handles that case
(`PRCreated: prURL != ""`).

## Testing

All tests extend the existing `writeMockScript` + local-bare-repo pattern in
`internal/github/adapter_test.go`. The gh mock script routes on argv (same
shell `case` pattern as `TestListIssuesWithLabels`).

1. **`TestUpsertBranchAndPR_Create`** — `gh api repos/.../branches/…` returns
   404; `gh pr list` returns `[]`. Expects `git push -u fork` (not force) and
   `pr create`. Final `Action == UpsertCreated`, `Branch == "agent/fix-1"`.

2. **`TestUpsertBranchAndPR_Update`** — branch api returns 200; `pr list`
   returns one open PR. Expects `git fetch`, `git push --force-with-lease`,
   `gh issue comment` with a timestamped body, no `pr create`. Returns the
   existing PR URL, `Action == UpsertUpdated`.

3. **`TestUpsertBranchAndPR_SuffixedWhenClosed`** — first lookup: branch
   exists, PR state `CLOSED`. Second lookup on `agent/fix-1--2`: branch does
   not exist. Expects push to `agent/fix-1--2` and PR creation against it.
   `Action == UpsertSuffixed`, `Branch == "agent/fix-1--2"`.

4. **`TestUpsertBranchAndPR_SkippedMerged`** — branch exists, PR state
   `MERGED`. Expects no `git push`, no `pr create`, no `pr comment`. Returns
   `UpsertResult{Action: UpsertSkippedMerged, PRURL: "", Branch: "agent/fix-1"}`,
   nil error.

5. **`TestUpsertBranchAndPR_SuffixCapExceeded`** — all of `agent/fix-1` and
   `agent/fix-1--2` through `agent/fix-1--10` resolve to closed PRs. Expects a
   non-nil error mentioning the cap.

The gh-mock scripts use shell `case` matching on `$*` (the command-line
arguments) to return different responses per gh invocation. For tests that
need distinct responses for different branch names (e.g.
`TestUpsertBranchAndPR_SuffixedWhenClosed`, which expects the base branch
lookup to return 200 + a closed PR and the `--2` suffix lookup to return 404
+ `[]`), the `case` patterns are ordered more-specific-first so shell's
first-match semantics pick the right response. This keeps tests deterministic
without an interface mock or external state (see
`internal/github/adapter_test.go`).

## Validation

Unit tests gate the merge. After merge:

1. Trigger a manual `workflow_dispatch` against issue #1268 (the actual
   failure case from CI run 24185014048). First run: `UpsertCreated`.
2. Trigger a second manual run against the same issue. Expected:
   `UpsertUpdated` — the same PR gets a new comment and a force-pushed head.
3. Verify no second PR appears on the upstream.
4. The next scheduled 9am/3pm/9pm run no longer fails when it picks the same
   top-ranked issue.

The observation from step 2 is the acceptance criterion "Observed in an actual
scheduled run (two runs on the same issue)."

## Risks and alternatives considered

- **`--force` instead of `--force-with-lease`:** simpler, one less command,
  but loses the "did someone else push in the meantime" safety net. We chose
  lease to match the issue's preference and because the extra `git fetch` is
  cheap (single ref).
- **Timestamp-based suffix (`agent/fix-1-20260409-1018`) instead of `--2/--3`:**
  always unique in one shot, no state lookup needed, but every retried run
  becomes a new PR which defeats the "update existing PR" goal the issue
  explicitly calls out. Rejected.
- **Rewrite PR body on update instead of commenting:** would lose the
  original summary context. The comment approach preserves history and matches
  how human contributors update PRs.
- **Handling a non-open, non-closed, non-merged state:** GitHub's GraphQL enum
  is `OPEN | CLOSED | MERGED`. `CLOSED` covers "closed without merge";
  `MERGED` is its own state. No other values are possible, so the switch is
  exhaustive.
