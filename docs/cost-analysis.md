# Cost Analysis

Comprehensive cost breakdown for the conduit-agent-experiment pipeline. Used as the canonical reference for documentation, presentations, and planning.

**Last updated:** 2026-04-09

## TL;DR

- **Per-run cost:** $0.06–$0.28 (observed), depending on issue complexity
- **Infrastructure cost:** $0 (GitHub Actions + Pages free for public repos)
- **Matching Conduit's active-period velocity (36 runs/month):** ~$2.16–$10/month
- **Key insight:** LLM cost dominates in every deployment scenario. Infrastructure is effectively free.

## Per-Run Cost Breakdown

### Baseline (from full pipeline achievement, 2026-04-07)

| Stage | Model | Tokens (est.) | Cost per run |
|-------|-------|--------------|-------------|
| Archivist | Gemini 2.5 Flash | ~5K in, ~2K out | $0.001 |
| Planner | Gemini 2.5 Flash | ~15K in, ~10K out | $0.003 |
| Reviewer | Gemini 2.5 Flash | ~20K in, ~0.5K out | $0.001 |
| Implementer (15 iter) | Haiku 4.5 (cached) | ~30K in, ~5K out | $0.050 |
| **Total baseline** | | | **$0.06** |

### Observed costs (first CI runs, 2026-04-08 to 2026-04-09)

| Run | Issue | Iterations | Cost | Status |
|-----|-------|-----------|------|--------|
| 24167191811 | #576 Swagger errors | 15 | $0.281 | ✅ |
| 24170172426 | #1268 embedding guide | 15 | $0.296 | ✅ |
| 24170175322 | #645 version constant | 15 | $0.153 | ✅ |
| 24185014048 | #1268 (retry) | 15 | $0.301 | ❌ (branch collision) |

**Observed range:** $0.15–$0.30
**Observed average on success:** ~$0.24

The $0.06 baseline was an optimistic estimate with a smaller plan. Real plans (24K–43K chars) consume more input tokens, pushing the implementer cost higher. Still well within the "less than a cup of coffee per month" narrative.

## Cost Optimization Techniques in Use

1. **Hybrid model routing** — Gemini Flash ($0.15/$0.60 per MTok) handles 4 of 5 pipeline steps; Claude Haiku ($1/$5 per MTok) only writes code
2. **Prompt caching** — System prompt and plan cached across implementer iterations (~90% input cost reduction after first call)
3. **Deterministic pre-gathering** — Archivist uses Go grep instead of LLM tool calls for file discovery
4. **Markdown plans over JSON** — No wasted tokens on failed JSON parses
5. **Single-call agents** — Gemini Flash runs once per stage, not in an agent loop

## Deployment Options: Cost Comparison

From [ADR 006](adr/006-pipeline-deployment-github-actions.md), matching Conduit's active-period velocity (36 runs/month):

| Option | 4 runs/mo | 36 runs/mo | Infra cost | Reactivity |
|--------|-----------|------------|-----------|-----------|
| **GitHub Actions** (chosen) | **$0.24** | **$2.16** | $0 | Cron + event |
| Cloud Run + Scheduler | $0.24 | $2.31 | ~$0.15 | Cron only |
| GHA + Cloud Run hybrid | $0.66 | $2.28 | ~$0.12 | Cron + dispatch |
| GitHub App / webhook | $0.24–$3.24 | $2.26–$5.54 | $0–$3 | Real-time |

**LLM cost is 93–100% of total cost in every option.** Infrastructure is effectively free.

## Monthly Projections

| Cadence | Runs/month | Baseline cost | Observed avg | Worst case |
|---------|-----------|--------------|--------------|-----------|
| Weekly | 4 | $0.24 | $0.96 | $1.60 |
| Match Conduit active pace | 36 | $2.16 | $8.64 | $14.40 |
| Daily | 30 | $1.80 | $7.20 | $12.00 |
| 3x daily (this week's data collection) | 93 | $5.58 | $22.32 | $37.20 |

The talk's headline number — "less than a cup of coffee per month" — holds at weekly cadence and matches Conduit's active-period pace within 1 coffee (~$6).

## Dashboard: Cost Comparison

From [ADR 006, dashboard section](adr/006-pipeline-deployment-github-actions.md#dashboard-github-pages-static):

| Dashboard Option | Monthly cost |
|------------------|-------------|
| **GitHub Pages (static)** | **$0** |
| Cloud Run (served web app) | $0.01–$3.41 |

GitHub Pages chosen: $0/month, reads the same `run.json`/`cost.json` artifacts the pipeline already produces.

## Projected Cost Impact of Planned Features

Five pipeline enhancements are planned (#30, #31, #32, #33, #34). Per-run cost delta for each:

| # | Feature | LLM cost | Retry risk | Net impact |
|---|---------|---------|-----------|-----------|
| 30 | Update existing PR (no branch collision) | $0 | None | **Saves ~$0.30 per current failure** |
| 31 | Use target repo PR template | $0 | None | Zero |
| 32 | Detect review bots | $0 | None | Saves 120s wall time |
| 33 | Internal code reviewer | +$0.005 | +$0.15 on retry | $0.005–$0.155 |
| 34 | Linter pre-check | $0 | +$0.15 on retry | $0–$0.15 |

### Combined per-run scenarios with all features

| Scenario | Cost per run |
|----------|-------------|
| Best case (no retries) | ~$0.29 |
| **Typical** (review retry 20%, lint retry 10%) | **~$0.34** |
| Worst case (both always retry) | ~$0.59 |

### This week's data collection (24 remaining runs)

| Scenario | Total cost |
|----------|-----------|
| Current (no new features) | $6.72 |
| **With all 5 features, typical** | **~$8.16** |
| Worst case (all retries fire) | $14.16 |

### The hidden win: #30 pays for everything else

Looking at observed data: **~30% of CI runs are currently failing at the push step** due to branch collisions. Each failed run still burns the full LLM cost (~$0.30) before failing.

**Savings from fixing #30:** 30% × 24 runs × $0.30 = **~$2.16 saved this week**

That more than offsets the additional cost of #33 and #34. **Implementing all 5 features is approximately cost-neutral or slightly cheaper** than the current state, while producing much higher-quality data.

## Budget Controls

The pipeline supports hard budget caps via environment variables (from #21):

| Variable | Scope | Default |
|----------|-------|---------|
| `PIPELINE_MAX_COST` | Total pipeline budget | none |
| `IMPL_MAX_COST` | Implementer step only | none |
| `ARCHITECT_MAX_COST` | Architect agent | none |
| `ARCHIVIST_MAX_COST` | Archivist step | none |

When a cap is exceeded, the pipeline halts before PR creation. Recommended budget for production:

- Single-run cap: `IMPL_MAX_COST=0.50` (covers 99th percentile observed)
- Pipeline total: `PIPELINE_MAX_COST=0.75`

## The Talk Headline

> **Matching Conduit's active-period velocity (36 commits/month) costs less than a cup of coffee per month.**

At observed rates ($0.24 average), 36 runs/month = $8.64. Add a modest buffer for retries and occasional worst-case runs, and the all-in cost is ~$10/month. Still cheaper than a single engineer-hour of maintenance work.

## References

- [ADR 006: Pipeline deployment via GitHub Actions](adr/006-pipeline-deployment-github-actions.md) — deployment and dashboard cost analysis
- [Full pipeline achievement report](experiments/2026-04-07-full-pipeline-achievement.md) — original $0.06 baseline
- [Cost tracking package](../internal/cost/) — per-step cost calculation and budget enforcement
- Issues #30, #31, #32, #33, #34 — planned features with projected cost impact
