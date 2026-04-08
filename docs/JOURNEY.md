# Project Journey: From Hypothesis to Working Pipeline

This document traces the full arc of the conduit-agent-experiment project -- what we set out to build, what we learned at each stage, the pivots we made, and where things stand now. It's written for a new collaborator joining the project.

## The thesis

Open source projects often lose maintainer bandwidth over time. ConduitIO/conduit, a streaming data platform, saw its commit velocity drop from ~36 commits/month during its active period to ~8 commits/month, while 130+ issues remained open. We hypothesized that a multi-agent AI system could help sustain that velocity -- not by replacing maintainers, but by autonomously handling the low-risk, well-scoped issues that pile up when humans get busy.

The experiment is also material for an [OSA Community talk](docs/design.md) on whether AI can extend the "maintenance half-life" of open source software.

## Milestones

### Milestone 0: Project setup (PR #1 precursor)

- Created the repo, separate from Conduit itself ([ADR 001](adr/001-separate-repo.md))
- Set up Go module, configs, and the basic pipeline structure
- Decision: the agent system operates *on* Conduit as a read-only target, never embedding inside it

### Milestone 1: Low-risk task loop (PR #1)

Built the first end-to-end pipeline: **Archivist -> Triage -> Verifier -> Report**.

- **Archivist**: retrieves relevant files from the target repo using keyword search + LLM-assisted ranking
- **Triage**: classifies issues by difficulty and blast radius
- **Verifier**: runs `go build`, `go vet`, targeted test commands against a worktree
- **Report**: generates structured Markdown/JSON reports

All roles used Gemini Flash via OpenAI-compatible endpoint. No code generation yet -- this milestone proved we could explore, classify, and verify.

### Milestone 2: Narrow bug-fix pilot (PR #6)

Added code generation agents:

- **Implementer**: generates patches using an LLM agent with tool use
- **Architect**: reviews generated patches for correctness, style, and architectural fit
- **Task Selector**: ranks candidate issues by feasibility x demand score
- **GitHub integration**: branch creation, commit, draft PR via `gh` CLI

This is where we ran the first real experiments against Conduit issues.

### Experiments 01-03: First contact with reality (April 5, 2026)

Three experiments targeting real Conduit issues. All three failed, but each failure taught us something:

| # | Issue | What happened | Root cause |
|---|-------|---------------|------------|
| 01 | Docs drift (task-001) | Credible patch, but verifier failed | Environment issue (`diff` CLI missing), not agent fault |
| 02 | HTTP status codes (#576) | Correct architectural intent, build failed | Cross-file naming inconsistency -- agent invented symbols |
| 03 | Version constant (#645) | Focused run (2 files), architect caught CI anti-pattern | Generated script lost due to git diff limitation |

**Key finding: the dominant failure mode is hallucinated symbols.** The agent writes code referencing functions and constants that don't exist in the codebase. This persisted across all experiments and models.

### Milestone 3: Scorecards and symbol extraction (PRs #8, #9)

- **Symbol extractor** (PR #8): Go AST-based parser that builds a package inventory -- exported functions, types, constants per package. The intent: inject real symbols into the planner prompt so the LLM can't hallucinate imports.
- **Full scorecards** (PR #9): extended evaluation metrics for run quality assessment.

### The architecture pivot (April 6-7, 2026)

After experiments 01-05 showed that the original architecture (all Gemini Flash, ADK agent loops) was unreliable, we made a significant pivot:

**Old architecture:** ADK Go agent loops for all roles, Gemini Flash everywhere, JSON output format.

**New architecture:** Deterministic Go code for exploration, single LLM calls for analysis, Markdown for plans, and a hybrid model strategy.

Why the pivot:
1. **Gemini Flash ignores tool-calling instructions** in agent loops -- the archivist never called `save_dossier` despite explicit prompts. Single direct calls are reliable.
2. **JSON output with code content breaks** -- even Gemini's JSON mode can't handle Go source code (backticks, special chars). Markdown is natural for LLMs and handles code blocks.
3. **Cheap models for thinking, expensive models for writing** -- Gemini Flash does analysis at $0.005/run; Claude Haiku does mechanical code writing at $0.05/run.

### Triage agent (PR #11, merged)

Built a standalone ADK Go agent for issue triage:
- Uses Gemini Flash with three function tools: `list_issues`, `get_issue`, `save_ranking`
- Scans GitHub issues, classifies by type/difficulty, ranks by feasibility x demand
- Output: JSON task queue consumed by the implementer
- Cost: ~$0.004/run
- Deployable to Cloud Run with Cloud Scheduler (design in [ADR 005](adr/005-triage-agent-cloud-run.md))

### Implementer pipeline (PR #16, in review)

The current state of the art -- a full 5-stage pipeline:

```text
triage -> archivist -> planner -> reviewer -> implementer -> draft PR
```

**Components:**

| Stage | Model | Technique | Cost |
|-------|-------|-----------|------|
| Triage | Gemini Flash | ADK agent with tools | $0.004 |
| Archivist | Gemini Flash | Deterministic grep + single LLM call | $0.001 |
| Planner | Gemini Flash | Single call, Markdown output | $0.003 |
| Reviewer | Gemini Flash | Single call, JSON approve/reject | $0.001 |
| Implementer | Claude Haiku 4.5 | BetaToolRunner, 5 tools, 15 iterations | $0.05 |

**Implementer tools:**
- `read_file` / `write_file` -- with path traversal + symlink escape protection
- `list_dir` -- directory listing
- `search_files` -- grep with output truncation
- `run_command` -- allowlisted commands (go build/test/vet, make, git diff/status/log) with scrubbed environment

**Security hardening** (addressed during code review):
- `safePath` validates via `filepath.Rel` + `filepath.EvalSymlinks`, walking up to nearest existing ancestor for new files
- `run_command` rejects path-qualified executables, uses minimal env (no API key leakage)
- `search_files` uses `-e` flag for pattern safety, excludes `.git/`, caps output at 64KB
- Archivist `readFileContent` resolves symlinks before opening

### Live run: Issue #576 (April 7, 2026)

The first successful end-to-end run:

- **Issue:** Error codes need to be documented in Swagger
- **Time:** 3 minutes total
- **Cost:** $0.06
- **Result:** [ConduitIO/conduit#2451](https://github.com/ConduitIO/conduit/pull/2451) -- 3 files changed, +73/-39 lines
- **CI result:** Failed -- hallucinated error constants (same failure mode as experiments 02-05)

The PR demonstrated the pipeline works mechanically. The CI failure confirmed that hallucinated symbols remain the primary challenge, validating the need for symbol inventory injection into the planner prompt.

## What we've proven

1. **The pipeline works end-to-end** -- from issue triage to draft PR, fully autonomous
2. **Hybrid model routing saves 20x on cost** -- Gemini Flash for thinking, Claude for writing
3. **Single LLM calls beat agent loops for analytical tasks** -- more reliable than letting Gemini Flash drive tool loops
4. **Prompt caching is critical** -- the plan + system prompt are cached across implementer iterations (90% input cost reduction)
5. **Matching Conduit's active-period velocity would cost ~$2/month** -- less than a cup of coffee

## What still fails

1. **Hallucinated symbols** -- the persistent failure mode. The planner writes plans referencing functions that don't exist, and the implementer faithfully writes the code. Symbol inventory injection (issue #7, PR #8) is the designed mitigation.
2. **No CI feedback loop** -- the pipeline creates a PR but doesn't read CI results or iterate. Issue #18 proposes this.
3. **No automated code review** -- issue #17 proposes Greptile integration.
4. **Environment-specific test flakiness** -- `make test` on the local Conduit checkout is unreliable due to missing tools and parallelism-sensitive tests.

## Open issues and next steps

| Issue | Title | Priority | Status |
|-------|-------|----------|--------|
| #12 | Epic: Hybrid Architecture Pipeline | -- | Tracking epic |
| #17 | Integrate Greptile for automated code review | High | Open |
| #18 | Add review feedback response loop | High | Open |
| #21 | Per-step cost tracking and budget controls | Medium | Open |
| #14 | Deploy triage agent to Cloud Run | Medium | Open |
| #19 | Human-in-the-loop integration points | Medium | Open |
| #20 | Project onboarding guide | Low | Open |
| #15 | Retry/reflect plugin for tool resilience | Low | Open |
| #13 | Wire triage output to Claude Code implementer | Low | Open |

## Key architectural decisions

- **[ADR 001](adr/001-separate-repo.md):** Agent system lives in its own repo, not inside Conduit
- **[ADR 005](adr/005-triage-agent-cloud-run.md):** Triage agent deploys to Cloud Run with Cloud Scheduler

## Technology stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Language | Go | Matches Conduit's stack; strong toolchain for AST parsing |
| Triage | ADK Go + Gemini Flash | Google's agent framework for tool-using agents |
| Archivist/Planner/Reviewer | Gemini Flash (direct calls) | Cheap, fast, reliable for single-call analysis |
| Implementer | anthropic-sdk-go + Claude Haiku | BetaToolRunner for iterative coding with compile-check |
| GitHub ops | `gh` CLI | Issue fetching, branch creation, PR management |
| Config | Viper + YAML | Standard Go configuration management |

## For new collaborators

1. **Read the README** for setup instructions
2. **Skim this document** for the full arc
3. **Read the [experiment reports](experiments/)** for detailed run evidence
4. **Check [open issues](https://github.com/William-Hill/conduit-agent-experiment/issues)** for what needs doing
5. **Run `make test`** to verify your local setup
6. The most impactful next step is **#18 (review feedback loop)** -- teaching the pipeline to read CI failures and iterate
