# Milestone 2: Narrow Bug-Fix Pilot

## Goal

Extend the Milestone 1 pipeline with three new agents (Implementer, Architect, Task Selector) and GitHub integration to run end-to-end from issue selection through draft PR creation. Prove the system can select real Conduit issues, generate narrow patches, validate them, review them architecturally, and open draft PRs for human review.

## Scope

- Task Selector agent that scans GitHub issues and ranks candidates
- Implementer agent that produces patch plans and generates code changes
- Architect agent that reviews patches against system boundaries and ADR guidance
- GitHub adapter for issue reading, branch creation, pushing, and draft PR creation
- Per-run evaluation and measurement data
- 3-5 real Conduit issues as pilot tasks
- Configurable target repo (fork for testing, upstream later)

## Out of Scope

- Automatic retry loops (Architect says "revise" -> run stops)
- PM-like agent with competitive analysis, security advisory scanning, feature prioritization
- Auto-merge or any non-draft PR actions
- Full aggregate scorecard with historical trends (Milestone 3 — light version included in M2)
- Human review time tracking automation (manual JSON in M2)

## Architecture

### Commands

Two CLI commands:

- `select` -- scans GitHub issues via `gh` CLI, filters/ranks by criteria, writes task JSONs to `data/tasks/`
- `run` (extended) -- loads a task JSON, runs: Archivist -> Triage -> Implementer -> Verifier -> Architect -> GitHub PR creation -> Report

### New Agents

- **Task Selector** (`internal/agents/selector.go`) -- LLM-assisted issue ranking
- **Implementer** (`internal/agents/implementer.go`) -- two-phase: patch plan then full file generation
- **Architect** (`internal/agents/architect.go`) -- reviews patch against dossier + supplemental ADRs for changed files

### New Infrastructure

- **GitHub adapter** (`internal/github/adapter.go`) -- wraps `gh` CLI for issue reading, branch creation, pushing, and draft PR creation
- **Evaluation** (`internal/evaluation/`) -- captures measurement data per run

### Config Changes

`experiment.yaml` gains a `github` section:

```yaml
github:
  owner: "ConduitIO"
  repo: "conduit"
  fork_owner: "William-Hill"
  base_branch: "main"
```

Policy gains `max_files_changed` to enforce narrow diffs.

## Pipeline Flow

```
1. Load task JSON
2. Build keyword dossier (M0 code)
3. Archivist: enhance dossier via LLM
4. Triage: accept/reject/defer
   -> reject/defer: write report, stop
5. Setup worktree from target repo
6. Implementer Phase 1: patch plan (reads files from worktree for context)
   -> too many files: "patch too broad" rejection, stop
7. Implementer Phase 2: generate full files, write to worktree
   -> all files fail: "implementation_failure", stop
8. Verifier: run tests/lint in worktree
9. Architect: review diff + dossier + supplemental ADRs
   -> reject: write report, stop
   -> revise: write report, stop (no auto-retry in M2)
10. Create branch, commit, push to fork
11. Open draft PR with review packet as body
12. Write artifacts: run.json, dossier.json, evaluation.json, report.md
13. Print summary + PR URL to stdout
```

## Component Details

### Task Selector Agent

The `select` command scans a GitHub repo's open issues and produces ranked task JSONs.

**How it works:**

1. Fetch issues via `gh issue list --repo <owner>/<repo> --state open --limit 100 --json number,title,labels,body,createdAt,comments,assignees`
2. Pre-filter: exclude issues that are assigned, have "epic" or "arch-v2" labels, or are clearly beyond L2 scope (body length > 2000 chars, mentions "redesign", "breaking change", etc.)
3. LLM ranking: send the filtered issue list to the LLM with criteria:
   - Is this a bug fix, docs issue, config mismatch, dependency bump, or narrow improvement?
   - Can it be resolved with changes to 5 or fewer files?
   - Are reproduction steps or acceptance criteria clear?
   - Estimated difficulty (L1/L2/L3/L4)?
   - Estimated blast radius (low/medium/high)?
4. LLM returns structured JSON: ranked list with difficulty, blast radius, rationale, and suggested acceptance criteria
5. Output: write top N task JSONs to `data/tasks/`, one per issue. Print summary table to stdout.

**CLI interface:**

```
go run ./cmd/experiment select --limit 5
go run ./cmd/experiment select --limit 10 --labels bug,docs
```

Reads `github.owner` and `github.repo` from `experiment.yaml`.

**Deferred to PM agent milestone:** competitive analysis, security advisory scanning, user request aggregation, feature prioritization beyond issue metadata.

### Implementer Agent

Two-phase agent that produces a patch plan then generates code changes.

**Phase 1 -- Patch Plan:**

The Implementer receives the task + enhanced dossier. It sends an LLM call with:
- Task description and acceptance criteria
- Dossier summary, relevant files, risks, open questions
- Contents of the most relevant files (top 10 from dossier, read from worktree)

The LLM returns structured JSON:

```json
{
  "plan_summary": "One-paragraph description of the approach",
  "files_to_change": [
    {
      "path": "pkg/provisioning/service.go",
      "action": "modify",
      "description": "Catch version-parse errors per document and continue loop"
    }
  ],
  "files_to_create": [],
  "design_choices": ["Isolate error per document rather than failing batch"],
  "assumptions": ["No other callers depend on the fail-fast behavior"],
  "test_recommendations": ["Add test with multi-doc YAML where one has invalid version"]
}
```

Policy check: if `len(files_to_change) + len(files_to_create)` exceeds `policy.max_files_changed`, the run stops with a "patch too broad" rejection.

**Phase 2 -- Code Generation:**

For each file in the plan:
- Read the current file contents from the worktree
- Send to LLM with: the patch plan, task context, and file contents
- LLM returns the complete updated file contents
- Write the new file to the worktree

For new files: LLM generates the full file, written to worktree.

After all files are written, run `git diff` in the worktree to produce the unified diff. This diff becomes the canonical patch artifact stored in the run output.

**Error handling:**
- If LLM returns content that does not parse or is identical to the original: log error, mark file as "generation failed", continue with remaining files
- If all files fail: mark run as failed with "implementation_failure" taxonomy entry
- If some succeed: proceed with partial patch, flag incomplete files in the Architect review

**Token management:** Files larger than 32KB are truncated with a marker. If a file to modify exceeds this, the Implementer logs a warning and the Architect is told the full file was not in context.

### Architect Agent

Reviews the patch for architectural alignment and semantic safety.

**Inputs:**
- The unified diff from the Implementer
- The Archivist's dossier
- The Implementer's patch plan (files changed, rationale, assumptions)
- The Verifier's report (command results, pass/fail)
- Supplemental context: for each file the Implementer actually touched, search for ADRs and design docs that reference that file's package or subsystem

**LLM prompt asks the Architect to evaluate:**
1. Does the patch stay within the subsystem's boundaries?
2. Does it contradict any ADR guidance?
3. Are there semantic risks (behavior changes, compatibility breaks, concurrency concerns)?
4. Is the diff minimal and reviewable?
5. Does the Verifier report support confidence in the change?
6. Are the Implementer's stated assumptions valid?

**LLM returns structured JSON:**

```json
{
  "recommendation": "approve",
  "confidence": "high",
  "alignment_notes": "Change is contained to provisioning loop error handling",
  "risks_identified": [],
  "adr_conflicts": [],
  "suggestions": ["Consider adding a log line when skipping an invalid document"],
  "rationale": "Narrow fix, consistent with existing per-document error handling pattern"
}
```

`recommendation` is one of: `approve`, `revise`, `reject`.

**Behavior based on recommendation:**
- **approve** -- pipeline continues to PR creation
- **revise** -- run stops, review packet is written to output, no PR is created. Report notes what the Architect wanted changed.
- **reject** -- run stops, failure taxonomy records "architecture_rejection", full report written

### GitHub Adapter

Wraps the `gh` CLI via `exec.Command`. No Go SDK dependency.

```go
type GitHubAdapter struct {
    Owner      string // e.g. "ConduitIO"
    Repo       string // e.g. "conduit"
    BaseBranch string // e.g. "main"
    ForkOwner  string // e.g. "William-Hill"
}

func (g *GitHubAdapter) ListIssues(ctx context.Context, opts IssueListOpts) ([]Issue, error)
func (g *GitHubAdapter) GetIssue(ctx context.Context, number int) (*Issue, error)
func (g *GitHubAdapter) CreateBranch(ctx context.Context, name string) error
func (g *GitHubAdapter) CommitAndPush(ctx context.Context, branch, message string) error
func (g *GitHubAdapter) CreateDraftPR(ctx context.Context, pr DraftPRInput) (string, error)
```

**Branch naming:** `agent/task-<issue-number>-<short-slug>` (e.g., `agent/task-2255-yaml-provisioning-fix`)

**Draft PR body contains the full review packet:**

```markdown
## Task
<task title and link to original issue>

## Dossier Summary
<Archivist's summary, relevant files, risks>

## Patch Plan
<Implementer's plan summary, design choices, assumptions>

## Verification Results
<Verifier pass/fail table with command outputs>

## Architect Review
<recommendation, confidence, alignment notes, risks, suggestions>

---
> Generated by conduit-agent-experiment run <run-id>
```

**Git operations in the worktree:**
1. Implementer writes files to worktree
2. Verifier runs commands in worktree
3. After Architect approves: `git checkout -b <branch>`, `git add`, `git commit`, `git push` to fork
4. `gh pr create --draft` targeting `owner/repo` base branch from `fork-owner/repo` head branch

### Evaluation and Measurement

**Per-run evaluation data model:**

```go
type Evaluation struct {
    RunID               string        `json:"run_id"`
    TaskID              string        `json:"task_id"`
    IssueNumber         int           `json:"issue_number"`
    Difficulty          string        `json:"difficulty"`
    BlastRadius         string        `json:"blast_radius"`
    TriageDecision      string        `json:"triage_decision"`
    ImplementerSuccess  bool          `json:"implementer_success"`
    FilesChanged        int           `json:"files_changed"`
    DiffLines           int           `json:"diff_lines"`
    VerifierPass        bool          `json:"verifier_pass"`
    ArchitectDecision   string        `json:"architect_decision"`
    ArchitectConfidence string        `json:"architect_confidence"`
    PRCreated           bool          `json:"pr_created"`
    PRURL               string        `json:"pr_url,omitempty"`
    FailureMode         string        `json:"failure_mode,omitempty"`
    FailureDetail       string        `json:"failure_detail,omitempty"`
    TotalDuration       time.Duration `json:"total_duration"`
    LLMCalls            int           `json:"llm_calls"`
    LLMTokensUsed       int           `json:"llm_tokens_used,omitempty"`
}
```

**Failure taxonomy:** uses the existing `FailureMode` enum (retrieval failure, task misclassification, implementation hallucination, semantically incorrect fix, test false confidence, architecture drift, environment/setup failure, insufficient repository context, excessive iteration cost, human rejection).

**Per-run output:** `evaluation.json` written alongside `run.json`, `dossier.json`, `report.md`.

**Aggregate scorecard** (`go run ./cmd/experiment scorecard`): reads all `evaluation.json` files, prints summary table. Light version in M2, full version in M3.

**Human review tracking:** manual JSON file (`data/evaluations/human-reviews.json`) for M2. Automation via PR comment parsing deferred to M3.

## Changes to Existing Components

- **Verifier** -- now runs in the worktree after the Implementer has written files, validating the patched state
- **Orchestrator** -- `RunWorkflow()` gains Implementer, Architect, and GitHub stages
- **Report** -- markdown gains Implementer plan, Architect review, and PR link sections
- **Config** -- gains `github` section and `policy.max_files_changed`
- **Models** -- Run struct gains implementer and architect fields

## What Stays Unchanged

- LLM client (`internal/llm/`)
- Retrieval/search (`internal/retrieval/`)
- Ingest/repo loader (`internal/ingest/`)
- Config loading mechanics
- Task, Dossier core structures (extended but backward compatible)

## Pilot Issues

| # | Issue | Type | Level |
|---|-------|------|-------|
| 2255 | Multi-pipeline YAML provisioning fails on invalid version | Bug fix | L1 |
| 576 | API returns HTTP 500 for everything -- needs proper 4xx codes | Error messaging | L1 |
| 645 | Automate version constant update in built-in connectors | CI/Housekeeping | L1 |
| 2061 | Pipeline `status: stopped` in config still runs | Bug / config alignment | L2 |
| 1999 | `LifecycleOnCreated` backwards compat broken | Bug | L2 |

## Testing Strategy

| Component | Test approach |
|-----------|--------------|
| Task Selector | Mock `gh` CLI output, test filtering/ranking logic, test LLM response parsing |
| Implementer Phase 1 | Mock LLM client, test plan JSON parsing, test policy max_files check |
| Implementer Phase 2 | Mock LLM client, test file write to temp dir, verify git diff output |
| Architect | Mock LLM client, test supplemental ADR retrieval, test recommendation routing |
| GitHub adapter | Mock `gh` CLI, test branch naming, PR body assembly, error handling |
| Evaluation | Test evaluation JSON writing, test scorecard aggregation |
| Integration | Mock LLM server + temp repo with git history, verify full pipeline from task to PR creation |

## Files to Create or Modify

| File | Action |
|------|--------|
| `internal/agents/selector.go` | Create |
| `internal/agents/selector_test.go` | Create |
| `internal/agents/implementer.go` | Create (currently empty) |
| `internal/agents/implementer_test.go` | Create |
| `internal/agents/architect.go` | Create (currently empty) |
| `internal/agents/architect_test.go` | Create |
| `internal/github/adapter.go` | Create (currently empty) |
| `internal/github/adapter_test.go` | Create |
| `internal/evaluation/metrics.go` | Create (currently empty) |
| `internal/evaluation/scorecard.go` | Create (currently empty) |
| `internal/evaluation/metrics_test.go` | Create |
| `internal/models/evaluation.go` | Modify (implement structs) |
| `internal/models/run.go` | Modify (add implementer/architect fields) |
| `internal/orchestrator/workflow.go` | Modify (add new pipeline stages) |
| `internal/orchestrator/workflow_test.go` | Modify (test new stages) |
| `internal/reporting/markdown_report.go` | Modify (new report sections) |
| `internal/config/config.go` | Modify (add GitHubConfig, max_files_changed) |
| `configs/experiment.yaml` | Modify (add github section) |
| `cmd/experiment/main.go` | Modify (add select and scorecard commands) |
| `data/tasks/task-002.json` through `task-006.json` | Create (pilot issue tasks) |
