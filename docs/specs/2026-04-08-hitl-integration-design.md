# Human-in-the-Loop (HITL) Integration Points

**Issue:** #19
**Epic:** #12
**Date:** 2026-04-08

## Overview

Define explicit human approval gates in the agent pipeline using GitHub-native mechanisms (labels, comments, `gh` CLI polling). The pipeline supports three operating modes to balance autonomy with human oversight.

## Operating Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `full` | Both Gate 1 and Gate 3 active | Production |
| `yolo` | No gates; pipeline runs autonomously, human only merges/closes draft PR | Demo, local dev |
| `custom` | Individual gate flags for fine-grained control | Testing, gradual rollout |

**Configuration:**
```
HITL_MODE=full|yolo|custom  (default: full)
```

In `yolo` mode, the pipeline behaves exactly as it does today — fully autonomous up to the draft PR creation, human just decides merge or close. No labels, no polling, no bot review loop.

In `custom` mode, individual `HITL_GATE*_ENABLED` flags take over.

## Gate 1: Issue Selection

**Purpose:** Human approves which issues the agent works on before any paid LLM calls.

### Labels

- `agent:candidate` — applied by triage agent when an issue is selected
- `agent:approved` — applied by human to approve for implementation
- `agent:rejected` — applied by human to reject

### Flow

1. Triage agent runs (scheduled or manual), scores and ranks issues as today.
2. For each selected issue, triage agent applies `agent:candidate` label via `gh` CLI.
3. Triage agent posts a comment on the issue with ranking rationale (difficulty, blast radius, confidence score) so the human has context.
4. Pipeline polls for `agent:approved` or `agent:rejected` label via `gh` CLI.
5. On `agent:approved` — pipeline picks up the task and proceeds to archivist → planner → implementer.
6. On `agent:rejected` — pipeline logs rejection in the run record, sets `run.HumanDecision = rejected`, skips the issue.

### Configuration

| Env Var | Type | Default | Description |
|---------|------|---------|-------------|
| `HITL_GATE1_ENABLED` | bool | true | Enable Gate 1. Ignored when `HITL_MODE` is not `custom`. |
| `HITL_GATE1_POLL_INTERVAL` | duration | 5m | How often to check for label changes when polling. |

## Gate 2: Plan Approval (Deferred)

**Status:** Deferred to a separate issue for future implementation.

**Rationale:** At ~$0.06/run, the cost of a bad plan is low. The existing planner → reviewer loop catches bad plans internally, and the architect review + draft PR give humans a chance to reject before merge. Gate 2 adds friction without proportional value at this price point.

**When to revisit:** If per-run costs increase significantly (e.g., using larger models) or if bad plans frequently make it through the reviewer to the implementer stage.

## Gate 3: PR Review with Bot Review Loop

**Purpose:** Automated code reviewers clean up the PR before a human sees it, then the human makes the final merge/close decision.

### Labels

- `agent:ready-for-review` — applied by responder when bot review cycle is clean, signals human to review

### Bot Review Triggers

Bot reviews are triggered by posting comments on the draft PR:

```yaml
bot_reviewers:
  - "@coderabbitai review"
  - "@greptile review"
```

The PR stays in **draft** throughout the bot review cycle. Bot reviews must be explicitly triggered after each fix push since draft PRs do not automatically trigger bot reviews.

### Flow

1. Implementer creates draft PR as today (with dossier summary, plan, verifier results, architect review in PR body).
2. Pipeline posts bot review trigger comments on the PR (e.g., `@coderabbitai review`, `@greptile review`).
3. Pipeline waits `HITL_BOT_REVIEW_WAIT` for bot review comments to arrive.
4. Responder classifies bot comments by severity, fixes actionable ones, pushes commits.
5. Responder resolves addressed conversation threads on the PR (configurable via `HITL_RESOLVE_BOT_COMMENTS`).
6. Responder re-triggers bot reviews by posting trigger comments again.
7. Loop repeats until: no new actionable comments, or `HITL_BOT_MAX_ITERATIONS` reached.
8. Responder applies `agent:ready-for-review` label to signal the human.
9. Human reviews the draft PR:
   - **Merge** → pipeline records `run.HumanDecision = approved`, done.
   - **Request changes** → responder picks up human comments, fixes, re-triggers bot reviews, loops back to step 4.
   - **Close** → pipeline records `run.HumanDecision = rejected`, done.
10. Pipeline polls PR state via `gh pr view` to detect human action.

### Configuration

| Env Var | Type | Default | Description |
|---------|------|---------|-------------|
| `HITL_GATE3_ENABLED` | bool | true | Enable Gate 3. Ignored when `HITL_MODE` is not `custom`. |
| `HITL_RESOLVE_BOT_COMMENTS` | bool | true | Resolve conversation threads after fixing bot comments. |
| `HITL_BOT_REVIEW_WAIT` | duration | 120s | Time to wait for bot reviews after triggering. |
| `HITL_BOT_MAX_ITERATIONS` | int | 3 | Max bot review → fix cycles before signaling human. |
| `HITL_GATE3_POLL_INTERVAL` | duration | 5m | How often to poll for human action on the PR. |

## Package Design: `internal/hitl/`

### `gates.go` — Gate Logic

- `WaitForLabel(ctx, repo, issueNumber, targetLabels []string, pollInterval) (label string, err error)` — polls for any of the target labels (e.g., `agent:approved` or `agent:rejected`) on an issue. Returns which label was found.
- `WaitForPRAction(ctx, repo, prNumber, pollInterval) (action string, err error)` — polls PR state for merged/closed/changes-requested. Returns the action taken.
- `ApplyLabel(ctx, repo, number, label) error` — applies a label via `gh`.
- `RemoveLabel(ctx, repo, number, label) error` — removes a label via `gh`.

### `comments.go` — Comment & Conversation Management

- `PostComment(ctx, repo, number, body) error` — posts a comment on an issue or PR.
- `TriggerBotReviews(ctx, repo, prNumber, triggers []string) error` — posts each trigger string as a comment on the PR.
- `ResolveThread(ctx, repo, prNumber, threadID) error` — resolves a review conversation thread via GitHub API.
- `GetUnresolvedThreads(ctx, repo, prNumber) ([]Thread, error)` — lists open review conversation threads.

### `config.go` — Configuration

Loads HITL settings from environment variables and/or YAML config. Resolves the operating mode (`full`, `yolo`, `custom`) and returns effective gate states.

```go
type Config struct {
    Mode              string        // full, yolo, custom
    Gate1Enabled      bool
    Gate1PollInterval time.Duration
    Gate3Enabled      bool
    Gate3PollInterval time.Duration
    ResolveBotComments bool
    BotReviewWait     time.Duration
    BotMaxIterations  int
    BotReviewers      []string      // trigger comments
}
```

## Orchestrator Integration

### Current Flow

```
Triage → Archivist → Planner → Reviewer → Implementer → Verifier → Architect → PR → Exit
```

### New Flow

```
Triage → [Gate 1] → Archivist → Planner → Reviewer → Implementer → Verifier → Architect → Draft PR → [Gate 3] → Exit
```

### Changes to `RunWorkflow` in `internal/orchestrator/workflow.go`

**After triage (Phase 0):**
- If Gate 1 enabled: apply `agent:candidate` label, post rationale comment, call `hitl.WaitForLabel()`.
- If rejected: set `run.FinalStatus = rejected`, `run.HumanDecision = rejected`, exit early.
- If not enabled: proceed immediately (current behavior).

**After PR creation (Phase 6):**
- If Gate 3 enabled: enter bot review loop:
  1. `hitl.TriggerBotReviews()`
  2. Wait `HITL_BOT_REVIEW_WAIT`
  3. Run responder (fix comments, resolve threads)
  4. Re-trigger bot reviews
  5. Repeat up to `HITL_BOT_MAX_ITERATIONS`
  6. Apply `agent:ready-for-review` label
  7. `hitl.WaitForPRAction()` — block until human acts
  8. Handle human action (merge/changes-requested/close)
- If not enabled: exit after PR creation (current behavior).

**Run record:** `HumanDecision` field set programmatically at each gate instead of remaining `pending`.

### Mode Behavior Summary

| Mode | Gate 1 | Gate 3 (Bot Loop) | Gate 3 (Human Wait) |
|------|--------|-------------------|---------------------|
| `full` | Active | Active | Active |
| `yolo` | Skip | Skip | Skip (human merges/closes manually) |
| `custom` | Per flag | Per flag | Per flag |

## Acceptance Criteria Mapping

| Criterion | How Addressed |
|-----------|---------------|
| Gate 1 and Gate 3 implemented | Gate 1 (label-based issue approval) and Gate 3 (bot loop + human review) |
| Human can approve/reject issue selection | `agent:approved` / `agent:rejected` labels on candidate issues |
| Human can merge/close the draft PR | Pipeline polls PR state, records decision in `run.HumanDecision` |
| Pipeline pauses and waits at each gate | `WaitForLabel()` and `WaitForPRAction()` poll loops with configurable intervals |
| Documentation of the HITL workflow | This spec + configuration reference |
