# Design: Review Feedback Response Loop (#18)

**Date:** 2026-04-08
**Author:** William Hill
**Status:** Approved

---

## Overview

A standalone CLI (`cmd/responder/main.go`) that takes a PR number, fetches review comments from all reviewers (Greptile, CodeRabbit, Codex), classifies them, batches actionable ones by file, dispatches the implementer agent to fix them, and pushes. Repeats up to N iterations until all comments are addressed or a reviewer approves.

## Components

## 1. Comment Fetcher (`internal/responder/comments.go`)

Fetches all review data for a given PR using the `gh` CLI:

- `gh api repos/{owner}/{repo}/pulls/{n}/comments` for inline review comments
- `gh pr view {n} --json reviews` to check for approvals
- Returns a structured list of `ReviewComment`:

```go
type ReviewComment struct {
    Author   string
    File     string
    Line     int
    Body     string
    Severity string // P1, P2, Major, Minor, Nitpick, or unknown
    Status   string // "pending", "addressed", "resolved"
}
```

Status detection:
- "addressed" if the comment body contains "Addressed in commit" or similar markers
- "resolved" if the GitHub conversation is resolved
- "pending" otherwise

## 2. Comment Classifier (`internal/responder/classify.go`)

Filters and groups comments for the fix agent:

1. **Filter out addressed/resolved comments** -- already handled
2. **Filter out nitpicks** -- CodeRabbit "Nitpick" severity, Greptile P3+, Codex items without a P-badge
3. **Group remaining by file path** -- so the agent can work file-by-file
4. **Sort by severity** -- P1/Critical first, then Major, then Minor

Output:

```go
type ActionableComment struct {
    File     string
    Line     int
    Body     string
    Author   string
    Severity string
}

func Classify(comments []ReviewComment) []ActionableComment
```

Severity normalization across tools:

| Tool | Critical | Major | Minor | Nitpick |
|------|----------|-------|-------|---------|
| Greptile | P1 | P2 | P3 | -- |
| CodeRabbit | Critical | Major | Minor | Nitpick |
| Codex | P1 badge | P2 badge | -- | -- |

## 3. Fix Agent

Reuses `internal/implementer` directly -- same `BetaToolRunner`, same 5 tools (read_file, write_file, list_dir, search_files, run_command), same model (Haiku 4.5 default, configurable).

Different system prompt:

```
You are a code review response agent. You receive review comments from
automated reviewers and must fix each one with minimal, targeted changes.

For each comment:
1. Read the file mentioned in the comment
2. Understand what the reviewer is asking for
3. Make the minimal fix
4. Run `go build ./...` to verify the fix compiles

Do NOT refactor surrounding code. Do NOT add features. Fix exactly what
the reviewer flagged, nothing more.
```

The actionable comments are formatted as a prompt section:

```
## Review Comments to Address

### internal/cost/budget.go (line 27) [Greptile, P1]
PLANNER_MAX_COST is loaded but never enforced -- no CheckStep("planner") call exists.

### internal/cost/budget.go (line 75) [Greptile, P2]
Malformed env var silently returns 0, disabling budget cap. Add a log warning.

...
```

## 4. Main Loop (`cmd/responder/main.go`)

```
responder --pr=22 [--max-iterations=3] [--model=haiku] [--wait=120]

for iteration := 1; iteration <= maxIterations; iteration++ {
    comments = fetchComments(prNumber)
    if hasApproval(comments) {
        log "PR approved, exiting"
        exit 0
    }
    actionable = classify(comments)
    if len(actionable) == 0 {
        log "No actionable comments remaining, exiting"
        exit 0
    }
    log "Iteration %d: %d actionable comments", iteration, len(actionable)

    // Clone or fetch the PR branch
    repoDir = cloneOrFetch(owner, repo, branch)

    // Run fix agent
    result = implementer.RunAgent(ctx, apiKey, model, repoDir, promptFromComments(actionable), maxToolIterations)

    // Commit and push
    commitAndPush(repoDir, branch, "fix: address review comments (iteration N)")

    // Wait for new reviews to arrive
    sleep(waitSeconds)
}
log "Max iterations reached, %d comments remain unresolved"
exit 1
```

## 5. Configuration

| Env var | Default | Description |
|---------|---------|-------------|
| `RESPONDER_PR_NUMBER` | required | PR number to respond to |
| `RESPONDER_MAX_ITERATIONS` | `3` | Max fix-push-review cycles |
| `RESPONDER_MODEL` | (Haiku 4.5) | Anthropic model for fix agent |
| `RESPONDER_WAIT_SECONDS` | `120` | Seconds to wait for new reviews after push |
| `ANTHROPIC_API_KEY` | required | API key for Claude |
| `IMPL_REPO_OWNER` | `ConduitIO` | Target repo owner |
| `IMPL_REPO_NAME` | `conduit` | Target repo name |
| `IMPL_FORK_OWNER` | `William-Hill` | Fork owner |

Makefile target:

```makefile
.PHONY: respond
respond:
	go run ./cmd/responder
```

## Scope Boundaries

**Does:**
- Fetches and classifies review comments from any GitHub PR reviewer
- Fixes actionable comments using the existing implementer agent and tools
- Pushes fixes and waits for new reviews
- Exits on approval, no remaining comments, or max iterations

**Does not:**
- Listen for webhooks (that's #24)
- Call the Greptile API (just reads comments posted via GitHub)
- Re-plan from scratch -- fixes are tactical patches, not architectural changes
- Reply to or resolve GitHub comments via API -- review tools detect addressed comments automatically from the pushed code
- Manage cost budgets -- the pipeline-level budget from #21 covers that

## Testing

- **Unit tests for classifier:** known comment formats from each tool (Greptile P1/P2 badges, CodeRabbit severity tags, Codex P-badges, "Addressed in commit" detection)
- **Unit tests for fetcher:** mock `gh` JSON output, verify correct parsing
- **Integration:** the fix agent reuses the existing implementer agent tests
- **Manual validation:** run against PR #22's comments as the first live test
