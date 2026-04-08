# Demo Guide

Step-by-step instructions for running the agent pipeline end-to-end. Can be run entirely from the terminal — no Claude Code session required.

## Prerequisites

1. Go 1.25+
2. `gh` CLI installed and authenticated (`gh auth status`)
3. Fork of the target repo (default: `William-Hill/conduit`) with push access
4. API keys in `.env` or exported:
   ```bash
   export ANTHROPIC_API_KEY=sk-ant-...
   export GEMINI_API_KEY=AIza...
   ```

## Quick Demo (~5 minutes, yolo mode)

Best for talks where you want to show the pipeline running without waiting for human approval gates.

```bash
# Source your API keys
set -a && source .env && set +a

# Run the full pipeline on a pre-selected issue
HITL_MODE=yolo IMPL_ISSUE_NUMBER=576 make implement
```

This will:
1. Skip Gate 1 (no human approval needed for issue selection)
2. Clone ConduitIO/conduit to a temp dir
3. Run archivist → planner → reviewer → implementer (~4 min)
4. Create a draft PR on the target repo
5. Skip Gate 3 (no bot review loop or human wait)

### What to watch for in the output

| Log line | What's happening |
|----------|-----------------|
| `[archivist] found N relevant files` | Gemini Flash explored the repo |
| `Plan produced (N chars)` | Gemini Flash wrote an implementation plan |
| `Plan approved` | A second Gemini Flash call validated the plan |
| `[iter N] tool: read_file/write_file/run_command` | Claude is writing code iteratively |
| `Agent completed in N iterations` | Code generation done |
| `Draft PR created: URL` | PR is live on GitHub |

### Pre-selected safe issues

These are L1-L2 documentation/config tasks with low blast radius — good demo candidates:

| Issue | Title | Difficulty | Type |
|-------|-------|-----------|------|
| #576 | Error codes needs to be documented in Swagger | L1 | docs |
| #1268 | Write a guide about embedding Conduit | L2 | docs |
| #1855 | Write a guide for setting up a pipeline | L2 | docs |

### Picking a code-change issue

For demos showing actual code changes (not just docs), find an open issue:

```bash
# Browse open issues on the target repo
gh issue list --repo ConduitIO/conduit --label bug --limit 20

# Pick one and run
HITL_MODE=yolo IMPL_ISSUE_NUMBER=<number> make implement
```

Look for issues tagged `bug` or `good first issue` with clear descriptions and narrow scope.

## Full Demo (~15 minutes, HITL mode)

Shows the human-in-the-loop approval flow — better for demonstrating production readiness.

### Step 1: Run the pipeline

```bash
set -a && source .env && set +a
HITL_MODE=full IMPL_ISSUE_NUMBER=576 make implement
```

### Step 2: Gate 1 — Approve the issue

The pipeline will:
- Apply `agent:candidate` label to the issue
- Post a triage rationale comment
- Log: `[HITL] Waiting for agent:approved or agent:rejected label...`

In another terminal or in the GitHub UI:
```bash
# Approve the issue
gh issue edit 576 --repo ConduitIO/conduit --add-label "agent:approved"
```

The pipeline resumes and runs archivist → planner → reviewer → implementer → draft PR.

### Step 3: Gate 3 — Bot review loop

After the draft PR is created, the pipeline:
- Triggers bot reviews (`@coderabbitai review`, `@greptile review`)
- Waits 2 minutes for reviews to arrive
- Fixes actionable comments automatically
- Resolves review threads
- Applies `agent:ready-for-review` label
- Waits for human action

### Step 4: Merge or close

In the GitHub UI, review the PR and either merge or close it. The pipeline logs the decision and exits.

## Running the Responder Separately

If you have an existing PR with review comments to address:

```bash
set -a && source .env && set +a
RESPONDER_PR_NUMBER=<pr-number> make respond
```

This fetches review comments, classifies by severity, runs a fix agent, pushes changes, resolves threads, and re-triggers bot reviews.

## Environment Variables Reference

### Required

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Claude API key (for implementer) |
| `GEMINI_API_KEY` or `GOOGLE_API_KEY` | Gemini API key (for archivist, planner, reviewer) |

### Pipeline configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `IMPL_REPO_OWNER` | `ConduitIO` | Target repo owner |
| `IMPL_REPO_NAME` | `conduit` | Target repo name |
| `IMPL_FORK_OWNER` | `William-Hill` | Fork to push branches to |
| `IMPL_ISSUE_NUMBER` | (top from triage) | Override issue selection |
| `IMPL_MODEL` | Haiku 4.5 | Anthropic model for code generation |
| `IMPL_MAX_COST` | (none) | Budget cap in dollars |

### HITL configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `HITL_MODE` | `full` | `full`, `yolo`, or `custom` |
| `HITL_GATE1_POLL_INTERVAL` | `5m` | How often to check for approval labels |
| `HITL_GATE3_POLL_INTERVAL` | `5m` | How often to check for PR actions |
| `HITL_BOT_REVIEW_WAIT` | `120s` | Wait time for bot reviews after triggering |
| `HITL_BOT_MAX_ITERATIONS` | `3` | Max bot review → fix cycles |
| `HITL_RESOLVE_BOT_COMMENTS` | `true` | Auto-resolve review threads after fixing |
| `HITL_BOT_REVIEWERS` | `@coderabbitai review,@greptile review` | Bot trigger comments |

### Responder configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `RESPONDER_PR_NUMBER` | (required) | PR to address review comments on |
| `RESPONDER_MAX_ITERATIONS` | `3` | Max fix → review cycles |
| `RESPONDER_WAIT_SECONDS` | `120` | Wait between iterations for new reviews |
| `RESPONDER_MODEL` | (Haiku 4.5) | Model for fix agent |

## Troubleshooting

**Push rejected (branch already exists)**
```bash
# Delete the stale branch on the fork
gh api repos/<fork-owner>/<repo>/git/refs/heads/agent/fix-<issue> -X DELETE
# Re-run
HITL_MODE=yolo IMPL_ISSUE_NUMBER=<issue> make implement
```

**Triage output is stale**
The triage agent is interactive (ADK Go console). Run `make triage` and interact with it, or use `IMPL_ISSUE_NUMBER` to bypass triage and pick an issue directly.

**Agent produces no changes**
The implementer may hit its iteration limit (15) without finishing. Check the summary output — it usually indicates what went wrong. Common cause: the plan was too ambitious for the iteration budget.

**Budget exceeded mid-run**
Set `IMPL_MAX_COST` to a budget cap. The pipeline will halt before PR creation if the budget is exceeded.

## Timing Reference

From the dry run on 2026-04-08 (issue #576, 16-file PR):

| Stage | Time |
|-------|------|
| Clone | ~2s |
| Archivist | ~16s |
| Planner | ~1m 35s |
| Reviewer | ~14s |
| Implementer | ~2 min |
| Push + PR | ~4s |
| **Total** | **~4 min** |

Bot review latency (if using Gate 3):
- Greptile: ~4 min
- CodeRabbit: ~6 min
