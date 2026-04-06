# Experiment Report: Agent-Assisted Maintenance for Conduit

**Author:** William Hill
**Date:** 2026-04-06
**Target project:** [ConduitIO/conduit](https://github.com/ConduitIO/conduit)
**Experiment repo:** [William-Hill/conduit-agent-experiment](https://github.com/William-Hill/conduit-agent-experiment)

---

## Executive Summary

We built a multi-agent pipeline to attempt bounded maintenance tasks on the Conduit open source streaming platform. Over 5 experiments, we identified and fixed three layers of agent failure modes — cross-file naming inconsistency, hallucinated imports, and syntax errors — progressively bringing a real bug fix (ConduitIO/conduit#576) from "build fails on 10 files" to "build passes, one missing brace." The core finding is not about any individual fix: **the limiting factor is agent architecture, not model capability.** Tool-using agents with iterative feedback loops (like Claude Code) succeed where single-shot text generators fail. This report documents the experiment results, analyzes the state of the art in autonomous OSS maintenance, and proposes a velocity-matched hybrid architecture that could sustain Conduit's maintenance pace for $4.50–45/month.

---

## Part 1: Experiment Results

### The Pipeline

We built a Go-based orchestrator with 5 agent roles: Triage, Archivist, Implementer, Verifier, and Architect. All roles used Gemini 2.5 Flash via the OpenAI-compatible API. The pipeline runs sequentially: select a task, build a context dossier, generate a patch, verify it (build, vet, test), and get an architectural review.

### What We Tested

| Exp | Task | Type | Files | Build | Key Finding |
|-----|------|------|-------|-------|-------------|
| 01 | Docs drift (YAML) | Docs | 1 | PASS | Environmental confounder corrupted verifier signal |
| 02 | HTTP status codes | Go code | 10 | FAIL | Cross-file naming inconsistency |
| 03 | CI version automation | Shell/YAML | 2 | PASS* | Force-push anti-pattern caught by architect |
| 04 | HTTP status codes (re-run) | Go code | 2 | FAIL | Hallucinated import (`pkg/config`) |
| 05 | HTTP status codes (+ inventory) | Go code | 5 | **PASS** | Test syntax error (missing brace) |

### The Progression

Each experiment revealed a distinct failure layer, and each fix eliminated it:

```
Experiment 02: NAMING INCONSISTENCY
  Files generated independently chose different symbol names.
  → Fix: share sibling file content between generation calls.

Experiment 04: HALLUCINATED IMPORT
  Agent imported a package that doesn't exist in Conduit.
  → Fix: inject package inventory from Go AST into the prompt.

Experiment 05: SYNTAX ERROR
  Generated test code has a missing brace.
  → Fix needed: compile-check feedback loop during generation.
```

By experiment 05, `go build` passed for the first time on a real multi-file Go code change. The agent's architectural intent was correct from experiment 02 — the failures were progressively more mechanical and more fixable.

### Pipeline Improvements Built

| Fix | What it does | Impact |
|-----|-------------|--------|
| JSON retry (architect, archivist, implementer) | Retries once on JSON parse failure with error feedback | Eliminated LLM response format flakiness |
| Per-task verifier commands | Tasks specify their own verification scope | Bypasses pre-existing environmental test failures |
| Baseline verifier | Runs commands before AND after patch, classifies failures | Distinguishes "env broke this" from "patch broke this" |
| New-file artifact capture | Reads and stores newly created files in run artifacts | Architect can review scripts, YAML, configs |
| Cross-file sibling content | Each generation call sees what earlier calls produced | Eliminated naming divergence between files |
| Package inventory | Injects Go AST-derived package + error sentinel list | Eliminated hallucinated imports |
| Architect → implementer revision loop | On "revise", re-generates with architect feedback | First autonomous self-correction cycle |

### The Fundamental Insight

**The experiment's architecture is the bottleneck, not the model.** Each agent gets one prompt and returns one response. There is no tool use, no iteration, no ability to check assumptions during generation.

Claude Code succeeds on these exact tasks because it has:
- **Active context acquisition** — reads files on demand, searches symbols, verifies imports
- **Compile-check iteration** — writes code, runs `go build`, sees errors, fixes them
- **Multi-turn reasoning** — explores the codebase before committing to an approach

The passive fixes we built (sibling content, package inventory) are workarounds for the lack of tools. Each one injects context the agent *would have found on its own* if it could read files. This approach hits diminishing returns — you can't predict everything the agent will need.

---

## Part 2: Prior Art — Who Else Is Doing This?

### Academic / Research

- **[SWE-agent](https://github.com/SWE-agent/SWE-agent)** (Princeton/Stanford, NeurIPS 2024): Takes a GitHub issue and autonomously fixes it. Uses tool-augmented LLM with a custom shell interface. State-of-the-art: Claude Sonnet 4.5 + Live-SWE-agent achieves 45.8% on SWE-Bench Pro. Focused on single-issue resolution, not ongoing maintenance.
- **[SWE-bench](https://github.com/SWE-bench/SWE-bench)**: The standard benchmark — 2,294 real GitHub issues from 12 Python repos. Evaluates whether an agent can produce a correct patch. Claude Opus 4.6 broke 80% on SWE-bench Verified (80.9%).
- **[OpenHands CodeAct](https://github.com/OpenHands/OpenHands)**: Open-source agent platform. 53% resolution rate on SWE-Bench. Designed for long-horizon tasks across entire repositories. Model-agnostic.

### Commercial Products

- **[GitHub Copilot Coding Agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent)** (GA since Sep 2025): Assign a GitHub issue to Copilot, it boots a VM, clones the repo, analyzes the codebase with RAG, implements the fix, opens a draft PR. Follows repo instructions and coding standards. Available on Pro, Pro+, Business, Enterprise plans.
- **[Devin](https://devin.ai/)** (Cognition): Fully autonomous software engineer with dedicated IDE. Pricing dropped from $500 to $20/month after Devin 2.0. Enterprise-focused.
- **[Sweep AI](https://sweep.dev/)**: "Junior AI Developer" living in your GitHub repo. Handles small bugs, docs, cleanup. Free tier: 15 tasks/day. Most generous free autonomous agent offering.
- **[CodeRabbit](https://coderabbit.ai/)**: Focused on code review rather than generation. Line-by-line feedback, catches logic mistakes. Free for open source.

### Open Source Frameworks

- **[SWE-agent](https://swe-agent.com/)**: Open source, works with any LLM. The reference implementation for "GitHub issue → autonomous fix."
- **[OpenHands](https://openhands.dev/)**: Open platform for cloud coding agents. Has its own agent SDK.
- **[Aider](https://aider.chat/)**: Terminal-based coding agent. Diff-based edits. Open source.
- **[Cline](https://github.com/cline/cline)**: IDE-based autonomous agent with review-first workflow.
- **[OpenClaw](https://github.com/pspdfkit/openclaw)**: Breakout 2026 project (210K+ stars). Combines local LLMs + Docker + GitHub as execution plane.

### What's NOT Been Done (Our Gap)

Most existing tools focus on **single-issue resolution**: given an issue, produce a patch. What we're proposing is different:

1. **Ongoing, scheduled maintenance** — not one-shot, but continuous
2. **Velocity matching** — calibrated to the repo's historical pace
3. **PM-like intelligence** — issue triage, feature proposal, backlog management
4. **Cost-optimized hybrid** — cheap models for triage, expensive models for code
5. **Open source sustainability framing** — explicitly addressing maintainer burnout

GitHub Copilot Coding Agent comes closest (assign issue → get PR), but it's reactive (you assign issues) and commercial. Nobody has built a **proactive, velocity-matched, cost-optimized maintenance agent for open source sustainability** — which is the unique contribution of this project.

---

## Part 3: The Conduit Velocity Analysis

### Historical Pace

| Period | Commits/month | Character |
|--------|--------------|-----------|
| Active (Jan 2024 – Jun 2025) | **36/month** | Full team, features + fixes + deps |
| Decline (Jul 2025 – Mar 2026) | **8/month** | Mostly automated dep bumps, near-zero human work |

The decline is a 78% drop. The 75 commits over the decline period are overwhelmingly `go.mod:` bumps (119 of ~170 commits in the last 12 months). Actual human-authored work has effectively stopped.

### The Backlog

130 open issues:
- **52 feature requests** — community wants new capabilities
- **47 connector requests** (28 destination + 19 source) — the ecosystem is asking for growth
- **7 bugs** — small but real
- **5 housekeeping** — cleanup opportunities
- **4 documentation** — low-hanging fruit

The project isn't dead — people want things from it. The capacity disappeared.

### Cost to Match Velocity

| Target | Runs/month | Gemini Flash | Hybrid | Claude |
|--------|-----------|-------------|--------|--------|
| Match active pace (36/mo) | 30 | **$4.50/mo** | **$12/mo** | **$45/mo** |
| Match decline pace (8/mo) | 8 | **$1.20/mo** | **$3.20/mo** | **$12/mo** |
| Exceed active pace (2/day) | 60 | **$9/mo** | **$24/mo** | **$90/mo** |

**To sustain the same velocity as Conduit's active period using agents costs less than a Netflix subscription.** At the low end with Gemini Flash, it's the cost of a coffee per month.

---

## Part 4: Proposed Architecture — Velocity-Matched Hybrid

### Design Principles

1. **Match, don't exceed.** Calibrate agent work rate to the repo's historical velocity. Don't flood the repo with AI PRs — match the pace the community was accustomed to.
2. **Cheap for triage, smart for code.** Use inexpensive models (Gemini Flash) for research, classification, and reporting. Use capable models (Claude) only for actual code generation.
3. **Tool-using, not text-generating.** Agents must read files, run builds, and verify during generation — not after.
4. **Human approval gate.** Draft PRs, never auto-merge. Humans review and decide.
5. **Observable.** Every run produces evaluation artifacts, scorecards, and experiment log entries.

### Architecture

```
┌─────────────────────────────────────────────────────┐
│  PM Agent (ADK Go + Gemini Flash)                   │
│  Schedule: weekly cron                              │
│  - Scan GitHub issues, discussions, stars, forks    │
│  - Classify: bug / feature / housekeeping / docs    │
│  - Rank by feasibility × community demand           │
│  - Match to velocity target (e.g., 8 tasks/month)  │
│  - Output: ranked task queue                        │
│  Cost: ~$0.20/month                                 │
└─────────────────────┬───────────────────────────────┘
                      │ task queue (JSON files or GitHub project board)
┌─────────────────────▼───────────────────────────────┐
│  Triage Agent (ADK Go + Gemini Flash)               │
│  Schedule: triggered per task from queue             │
│  - Assess difficulty, blast radius                  │
│  - Build dossier (context, symbols, related files)  │
│  - Decide: attempt / defer / escalate to human      │
│  Cost: ~$0.05/task                                  │
└─────────────────────┬───────────────────────────────┘
                      │ accepted tasks only
┌─────────────────────▼───────────────────────────────┐
│  Implementer (Claude Code Remote Tasks or           │
│  Claude Agent SDK — full tool use + iteration)      │
│  Schedule: triggered per accepted task              │
│  - Read files, search code, run builds iteratively  │
│  - Generate patch with compile-check loop           │
│  - Run tests, fix failures, verify                  │
│  - Open draft PR when passing                       │
│  Cost: ~$1-2/task                                   │
└─────────────────────┬───────────────────────────────┘
                      │ draft PR + run artifacts
┌─────────────────────▼───────────────────────────────┐
│  Evaluation (existing scorecard framework)          │
│  - Record metrics, failure taxonomy                 │
│  - Generate experiment log entry                    │
│  - Update aggregate scorecard                       │
│  Cost: ~$0.01/task                                  │
└─────────────────────────────────────────────────────┘
```

### Technology Choices

| Layer | Technology | Why |
|-------|-----------|-----|
| PM Agent | ADK Go + Gemini Flash on Cloud Run | Go-native, cheap ($0.30/1M tokens), schedulable via Cloud Scheduler |
| Triage Agent | ADK Go + Gemini Flash | Same infra as PM, `functiontool` for symbol lookup |
| Implementer | Claude Code Remote Tasks or Claude Agent SDK (Go) | Full tool use, iteration, highest code quality |
| Evaluation | Existing Go code (scorecard, taxonomy) | Already built and tested |
| Scheduling | Cloud Scheduler (GCP) or GitHub Actions cron | Free for public repos, or minimal GCP cost |

### What We Keep from the Current Project

- Evaluation framework (scorecard, failure taxonomy, experiment logs)
- Task model, run model, dossier structure
- Symbol extractor + package inventory (feeds triage agent)
- The IMRAD experiment documentation methodology
- The velocity analysis and cost modeling

### What We Replace

| Current | Replacement |
|---------|------------|
| Single-shot text generation | Tool-using agents with iteration |
| Manual triggering | Scheduled PM + event-driven triage |
| Sequential pipeline (fixed stages) | Adaptive agent with compile-check loops |
| Gemini-only | Hybrid: Gemini Flash (triage) + Claude (code gen) |
| Local-only execution | Cloud Run (triage) + Claude Remote Tasks (code) |

---

## Part 5: Comparison of Approaches

### Full Options Matrix

| Approach | Tool use | Iteration | Deploy | Cost/month | Quality | Setup |
|----------|----------|-----------|--------|-----------|---------|-------|
| **Current experiment** | None | Post-hoc only | Local | $5-15 (Gemini) | Low (build fails) | Already built |
| **Claude Code Remote Tasks** | Full | Full | Anthropic cloud | $45-150 | Highest | Minimal |
| **ADK Go + Gemini (Cloud Run)** | `functiontool` | `loopagent` | Cloud Run | $5-15 | Good | Significant |
| **Hybrid (proposed)** | Full (Claude for code) | Full | Cloud Run + Anthropic | $12-45 | High | Moderate |
| **GitHub Copilot Agent** | Full | Full | GitHub cloud | $19/user/mo | High | Minimal |
| **SWE-agent (self-hosted)** | Shell tools | Agent loop | Self-hosted | API cost only | Good (45.8% SWE-bench) | Moderate |
| **OpenHands** | CodeAct tools | Full | Self-hosted or cloud | API cost only | Good (53% SWE-bench) | Moderate |
| **Sweep AI** | GitHub integration | Iterative | SaaS | Free (15 tasks/day) | Moderate | Minimal |
| **Local LLMs (Ollama)** | Limited | Limited | Self-hosted | $0 (hardware) | Low for complex tasks | Significant |

### For the OSA Talk: The Unique Angle

Existing tools solve **"given an issue, produce a patch."** Our project asks a different question: **"Can agents sustain a project's maintenance velocity after human bandwidth drops off, at a cost proportional to the project's resources?"**

This reframes AI agents from a productivity tool to a **sustainability mechanism for open source.** The velocity-matching approach, the cost analysis, and the explicit framing around maintainer burnout are the novel contributions.

---

## Part 6: Key Quotes for the Slide Deck

> "To sustain the same velocity as Conduit's active period using agents costs $4.50 to $45 per month — less than a Netflix subscription."

> "The limiting factor is not model capability. It's agent architecture. Give the same model tools and iteration, and three layers of failure modes disappear."

> "We don't try to outpace the humans. We match their historical cadence and ask: what does it cost for agents to sustain that pace?"

> "The contribution process has evolved from two parties (maintainers and contributors) to three: maintainers, contributors, and contributors' agents."

> "130 open issues. 52 feature requests. 47 connector requests. The project isn't dead — people want things from it. The capacity disappeared."

---

## Appendix: Sources

### Experiment Data
- Run artifacts: `data/runs/` (5 experiment directories)
- Experiment logs: `docs/experiments/` (README + 5 IMRAD writeups)
- Pipeline fix commits: `feature/experiment-failure-fixes` branch

### Prior Art
- [SWE-agent (Princeton/Stanford)](https://github.com/SWE-agent/SWE-agent)
- [SWE-bench benchmark](https://github.com/SWE-bench/SWE-bench)
- [OpenHands CodeAct](https://github.com/OpenHands/OpenHands)
- [GitHub Copilot Coding Agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent)
- [Devin by Cognition](https://devin.ai/agents101)
- [Sweep AI](https://sweep.dev/)
- [CodeRabbit](https://coderabbit.ai/)
- [Aider](https://aider.chat/)
- [OpenClaw](https://github.com/pspdfkit/openclaw)

### Architecture Research
- [Claude Code Remote Tasks](https://code.claude.com/docs/en/scheduled-tasks)
- [Claude Agent SDK](https://platform.claude.com/docs/en/agent-sdk/overview)
- [Community Claude Agent SDK for Go](https://github.com/severity1/claude-agent-sdk-go)
- [ADK Go 1.0](https://github.com/google/adk-go)
- [ADK Deploy to Cloud Run](https://docs.cloud.google.com/run/docs/ai/build-and-deploy-ai-agents/deploy-adk-agent)
- [Gemini API Pricing](https://www.tldl.io/resources/google-gemini-api-pricing)
- [Claude API Pricing](https://www.tldl.io/resources/anthropic-api-pricing)

### OSS Sustainability Context
- [AI and Open Source: A Maintainer's Take (2025)](https://st0012.dev/2025/12/30/ai-and-open-source-a-maintainers-take-end-of-2025/)
- [Predictions for Open Source in 2026: Maintainer Burnout](https://www.activestate.com/blog/predictions-for-open-source-in-2026-ai-innovation-maintainer-burnout-and-the-compliance-crunch/)
- [GitHub Actions 2026 Pricing Changes](https://github.com/resources/insights/2026-pricing-changes-for-github-actions)
