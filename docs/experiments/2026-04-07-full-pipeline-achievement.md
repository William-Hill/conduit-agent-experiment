# Full Pipeline Achievement: Triage to Draft PR

**Date:** 2026-04-07
**Author:** William Hill
**Target:** ConduitIO/conduit
**Result:** Draft PR created — [ConduitIO/conduit#2451](https://github.com/ConduitIO/conduit/pull/2451)

---

## Executive Summary

We built and demonstrated an end-to-end multi-agent pipeline that takes a GitHub issue and produces a draft PR — fully autonomously, in 3 minutes, for $0.06. This validates the hybrid architecture proposed after the initial experiments: cheap models for thinking, expensive models for writing.

## The Pipeline

```text
┌─────────────────────────────────────────────────────────┐
│  1. Triage Agent (ADK Go + Gemini Flash)                │
│     Scans GitHub issues, classifies, ranks by score     │
│     Output: ranked task queue (JSON)                    │
│     Cost: ~$0.004/run                                   │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│  2. Archivist (Go + Gemini Flash, single call)          │
│     Greps repo for issue keywords, Gemini analyzes      │
│     Output: dossier with file contents, approach, risks │
│     Cost: ~$0.001/run                                   │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│  3. Planner (Gemini Flash, single call)                 │
│     Writes markdown implementation plan with exact code │
│     Output: ~30K char plan with code blocks             │
│     Cost: ~$0.003/run                                   │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│  4. Reviewer (Gemini Flash, single call)                │
│     Validates plan addresses the issue, checks paths    │
│     Output: approved / revise with feedback             │
│     Cost: ~$0.001/run                                   │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│  5. Implementer (anthropic-sdk-go + Haiku 4.5)          │
│     Writes files, runs go build, fixes errors           │
│     Uses BetaToolRunner with 5 coding tools             │
│     Output: git diff, draft PR                          │
│     Cost: ~$0.05/run (with prompt caching)              │
└─────────────────────────────────────────────────────────┘
```

## Live Run: Issue #576

**Issue:** [Error codes needs to be documented in Swagger](https://github.com/ConduitIO/conduit/issues/576)
**Category:** Documentation (L1 difficulty)
**Score:** 54 (feasibility 9, demand 6)

### Step-by-Step Results

| Step | Agent | Model | Time | Output |
|------|-------|-------|------|--------|
| Triage | ADK Go agent | Gemini Flash | 45s | Ranked #576 as #2 of 4 suitable issues |
| Archivist | Go + Gemini | Gemini Flash | 21s | 7 relevant files identified |
| Planner | Gemini direct | Gemini Flash | 64s | 29,721 char markdown plan |
| Reviewer | Gemini direct | Gemini Flash | 19s | Approved |
| Implementer | BetaToolRunner | Haiku 4.5 | 62s | 3 files changed, +73/-39 lines |
| Push + PR | gh CLI | — | 3s | [PR #2451](https://github.com/ConduitIO/conduit/pull/2451) |

### Files Changed

```text
pkg/http/api/status/status.go | 50 +++++++++++++++++++++++++++++++------------
pkg/pipeline/errors.go        | 44 +++++++++++++++++++------------------
pkg/pipeline/service.go       | 18 ++++++++++++----
3 files changed, 73 insertions(+), 39 deletions(-)
```

## Cost Analysis

### Per-Run Cost Breakdown

| Component | Model | Token Est. | Cost |
|-----------|-------|-----------|------|
| Archivist | Gemini 2.5 Flash | ~5K input, ~2K output | $0.001 |
| Planner | Gemini 2.5 Flash | ~15K input, ~10K output | $0.003 |
| Reviewer | Gemini 2.5 Flash | ~20K input, ~0.5K output | $0.001 |
| Implementer (15 iter) | Haiku 4.5 | ~30K input (cached), ~5K output | $0.05 |
| **Total per run** | | | **~$0.06** |

### Monthly Projections

| Pace | Runs/month | Cost/month |
|------|-----------|------------|
| Match Conduit active pace (36/mo) | 36 | $2.16 |
| Weekly maintenance (4/mo) | 4 | $0.24 |
| Daily maintenance (30/mo) | 30 | $1.80 |

**The cost of maintaining an open-source project's velocity is less than a cup of coffee per month.**

### Cost Optimization Techniques Used

1. **Hybrid model routing** — Gemini Flash ($0.15/$0.60 per MTok) for thinking, Haiku ($1/$5 per MTok) for writing
2. **Prompt caching** — System prompt + plan content cached across implementer iterations (90% input cost reduction after first call)
3. **Deterministic pre-gathering** — Archivist uses Go code (grep) instead of LLM tool calls for repo exploration
4. **Markdown over JSON** — Eliminates failed JSON parsing retries that waste tokens

## Architecture Learnings

### What Worked

1. **Separating thinking from writing** — Gemini Flash does 4 of 5 pipeline steps at 1/20th the cost of Anthropic
2. **Markdown plans** — natural for LLMs, handles code blocks, no parsing issues
3. **Single-call agents** — more reliable than agent loops for Gemini Flash (which ignores tool-calling instructions)
4. **Pre-gathered context** — deterministic grep in Go is faster and more reliable than letting the LLM explore
5. **Prompt caching** — critical for iterative agents (implementer runs 15 iterations on the same base context)

### What Failed and Why

1. **ADK Go agent loop for archivist** — Gemini Flash repeatedly ignored `save_dossier` instructions, even with strict budgets. Replaced with deterministic Go code + single LLM call.
2. **JSON output with Go code** — Even Gemini's JSON mode can't handle Go source code (backticks, special chars). Switched to markdown.
3. **Haiku without pre-digested context** — Burned 20-30 iterations exploring instead of writing. Fixed by providing the complete plan upfront.
4. **Large open-ended issues** — Issue #1268 (write a full guide) was too ambitious. L1 tasks with clear scope work best.

### Key Architecture Decision: Agent Loops vs. Direct Calls

| Pattern | When to Use | Example |
|---------|------------|---------|
| **Agent loop** (tool-using) | Model needs to iteratively interact with the environment | Implementer (write, build, fix, retry) |
| **Direct call** (single prompt→response) | Task is analytical, all context can be provided upfront | Archivist, Planner, Reviewer |

The insight: only the implementer truly needs an agent loop. Everything else is a structured analysis task that works better as a single call.

## Technology Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Triage | ADK Go + Gemini Flash | Tool-using agent for GitHub exploration |
| Archivist | Go + Gemini Flash (direct) | Deterministic search + analytical LLM call |
| Planner | Gemini Flash (direct) | Structured analysis, markdown output |
| Reviewer | Gemini Flash (direct, JSON mode) | Simple approve/reject decision |
| Implementer | anthropic-sdk-go BetaToolRunner + Haiku | Iterative coding with compile-check loop |
| GitHub integration | `gh` CLI | Issue fetching, PR creation |
| Repo operations | `git clone`, `go build` | Standard toolchain |

## CI Results: PR #2451

The draft PR failed CI with **linting and test errors caused by hallucinated symbols**:

```text
pkg/http/api/status/status.go:61: undefined: connector.ErrNameAlreadyExists
pkg/http/api/status/status.go:63: undefined: connector.ErrPipelineIDMissing
pkg/http/api/status/status.go:80: undefined: processor.ErrNameMissing
pkg/http/api/status/status.go:82: undefined: processor.ErrIDMissing
pkg/http/api/status/status.go:84: undefined: processor.ErrNameAlreadyExists
pkg/http/api/status/status.go:86: undefined: processor.ErrParentIDMissing
```

The implementer created error constant references (`connector.ErrNameAlreadyExists`, `processor.ErrNameMissing`, etc.) that don't exist in the Conduit codebase. This is the **same hallucination failure mode** identified in experiments 02-05: the agent invents symbols that sound right but aren't defined.

### Root Cause

The planner (Gemini Flash) wrote an implementation plan that referenced these error constants. The implementer (Haiku) faithfully wrote the planned code without verifying the symbols exist. The `go build` check in the implementer's cloned repo would have caught this — but the implementer hit its 15-iteration limit before getting to the full build verification.

### Lessons

1. **The implementer must run `go build` BEFORE the iteration limit** — currently it can burn iterations on reading/writing and hit the cap before verifying
2. **The planner should only reference symbols that appear in the dossier** — the archivist provides file contents, so the planner should be grounded in those
3. **A post-PR CI check agent could fix these automatically** — read the lint errors, fix the undefined symbols, push again (this is exactly what #18 proposes)
4. **The package inventory from experiment 05 would prevent this** — injecting known symbols into the planner prompt eliminates hallucinated imports

This failure validates the need for:
- #17 (automated code review) — would catch this before human review
- #18 (review feedback loop) — would read the CI failure and fix it
- #21 (cost tracking) — would show how many iterations were "wasted" on exploration vs. writing

---

## What's Next

### Missing from Current Pipeline

1. **Code quality review** — No automated review of the PR before human review
2. **Review response loop** — Can't respond to automated review feedback and iterate
3. **Human-in-the-loop gates** — No explicit approval points beyond "draft PR"
4. **Project onboarding** — No standard way to configure the pipeline for a new project
5. **Cost tracking** — Per-step token usage not captured in run artifacts

### Proposed Next Phase

See GitHub issues for detailed tracking:
- Automated code review integration (Greptile)
- Review feedback loop (respond to review, iterate)
- Human-in-the-loop integration points
- Project onboarding guide
- Cost tracking and budgets
