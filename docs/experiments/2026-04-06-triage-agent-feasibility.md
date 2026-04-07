# Experiment: Triage Agent Feasibility Test

**Date:** 2026-04-06 (agent run crossed midnight UTC; output file dated 2026-04-07)
**Author:** William Hill
**Target:** ConduitIO/conduit

---

## Objective

Validate that an ADK Go agent with Gemini 2.5 Flash can autonomously triage GitHub issues for the Conduit project: fetch issues, classify them by type and difficulty, assess automation feasibility, rank by feasibility x community demand, and save a structured ranking to disk.

## Method

Built an ADK Go v1.0.0 agent with three function tools:
- `list_issues` — wraps `gh issue list` via existing `github.Adapter`
- `get_issue` — wraps `gh issue view` for full issue details
- `save_ranking` — persists ranked output as dated JSON

Agent instruction defines a 6-step workflow: fetch, classify, assess feasibility, get details on candidates, score, and save. Classification categories: bug, feature, connector, housekeeping, docs. Only bugs, housekeeping, and docs with L1-L2 difficulty and low-medium blast radius are marked suitable.

Ran via ADK Go web API mode (`web -port 8181 api`) with a single user message:
> "Run triage on the open issues for ConduitIO/conduit."

## Results

### Agent Behavior

The agent executed autonomously in a single turn with multiple tool calls:

| Step | Action | Detail |
|------|--------|--------|
| 1 | `list_issues(limit=100)` | Fetched 58 open issues |
| 2 | Classification | Analyzed all 58, filtered to 4 candidates |
| 3 | `get_issue` x4 (parallel) | Fetched details for #1855, #1268, #645, #576 |
| 4 | `save_ranking` | Wrote 4 ranked issues to `data/tasks/triage-2026-04-07.json` |

Total events: 8 (1 user message, 7 agent events including tool calls/responses)

### Ranked Output

| Rank | Issue | Category | Difficulty | Feasibility | Demand | Score |
|------|-------|----------|-----------|-------------|--------|-------|
| 1 | #1268 Write guide: embedding Conduit | docs | L2 | 9 | 7 | 63 |
| 2 | #576 Document error codes in Swagger | docs | L1 | 9 | 6 | 54 |
| 3 | #1855 Write guide: postgres->kafka pipeline | docs | L2 | 8 | 6 | 48 |
| 4 | #645 Automate version constant update | housekeeping | L2 | 8 | 5 | 40 |

### Quality Assessment

**Correct classifications:**
- Documentation tasks correctly identified as high-feasibility
- Feature/connector requests correctly excluded
- #645 (version constant automation) correctly classified as housekeeping

**Conservative scoring:**
- Agent was conservative as instructed — only 4 of 58 issues marked suitable
- Feasibility scores (8-9) are reasonable for docs/housekeeping tasks
- Demand scores reflect comment activity and clarity of the issues

**Parallel tool calls:**
- The agent called `get_issue` for 4 issues simultaneously (parallel function calls)
- This demonstrates ADK Go's tool-calling efficiency

### Performance

- Processing time: ~45 seconds (one LLM turn with tool calls)
- Token usage: ~15-20K input tokens (58 issue summaries), ~2K output tokens
- Estimated cost: ~$0.004 per triage run at Gemini 2.5 Flash pricing

## Conclusions

**ADK Go is viable for the PM/triage role.** Key findings:

1. **Tool-using agents work.** The agent autonomously chose when to list, when to drill into details, and when to save. No manual orchestration needed.

2. **Gemini 2.5 Flash is sufficient.** Classification quality is good. The agent correctly filtered 58 issues down to 4 actionable candidates with defensible rationale.

3. **Cost is negligible.** At ~$0.004/run, weekly triage costs $0.02/month. Even daily triage is under $0.15/month.

4. **ADK Go framework is solid.** v1.0.0 delivered on `functiontool`, launcher modes (console/web/api), and parallel tool calls. No API surprises.

5. **The architecture pivot is validated.** This single ADK Go agent replaces the old selector + triage pipeline with better results: it reads full issue details on demand, makes contextual decisions, and produces a richer output format.

## Next Steps

- Wire the ranked output into a Claude Code Remote Task for implementation
- Add `retryandreflect` plugin for tool error resilience
- Deploy to Cloud Run with Cloud Scheduler for weekly cron
- Expand the agent's assessment with repository context (file structure, recent PRs)
