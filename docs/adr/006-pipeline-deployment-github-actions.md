# ADR 006: Deploy Full Pipeline via GitHub Actions

## Status
Accepted

## Date
2026-04-08

## Context
The full pipeline (`make implement`) runs locally today. To achieve ongoing automated maintenance of ConduitIO/conduit, the pipeline needs to run autonomously — either on a schedule or reactively when the repo is active.

Issue #14 and ADR 005 cover deploying triage to Cloud Run. This ADR covers deploying the **full end-to-end pipeline** (triage → archivist → planner → reviewer → implementer → PR).

### Target cadence
Conduit's active-period commit velocity was ~36 commits/month. Matching that pace is the upper bound. A realistic starting point is 4 runs/month (weekly), scaling up as confidence grows.

### Pipeline cost baseline
Per the [full pipeline achievement report](../experiments/2026-04-07-full-pipeline-achievement.md):

| Stage | Model | Cost per run |
|-------|-------|-------------|
| Archivist | Gemini 2.5 Flash | $0.001 |
| Planner | Gemini 2.5 Flash | $0.003 |
| Reviewer | Gemini 2.5 Flash | $0.001 |
| Implementer (15 iter) | Haiku 4.5 | $0.050 |
| **Total** | | **$0.06** |

## Options Evaluated

### Option 1: GitHub Actions (cron + issue-event trigger)

A workflow in this repo triggered by a weekly cron and/or `issues: labeled` events.

| Monthly cost (4 runs) | Monthly cost (36 runs) |
|----------------------|----------------------|
| **$0.24** | **$2.16** |

Infrastructure cost is zero for public repos. For private repos, add ~$0.008/min × 4 min/run = $0.38/month at 36 runs.

**Pros:** Zero infrastructure, native secrets, activity-driven, free for public repos.
**Cons:** Requires PAT or GitHub App for cross-repo PR creation. Harder to test locally than Cloud Run.

### Option 2: Cloud Run + Cloud Scheduler

Extends ADR 005's approach to run the full pipeline on a weekly cron.

| Monthly cost (4 runs) | Monthly cost (36 runs) |
|----------------------|----------------------|
| **$0.24** | **$2.31** |

Additional infra: ~$0.10 Cloud Run compute + ~$0.05 Artifact Registry at 36 runs. Scheduler and Secret Manager stay in free tier.

**Pros:** Proven path (ADR 005), long-running job support (24h), clean GCP separation.
**Cons:** Requires GCP project + IAM + secrets. Clock-based only (not reactive to repo activity). More ops overhead.

### Option 3: Hybrid (Cloud Run triage + GitHub Actions implementer)

Triage runs daily on Cloud Run (cheap Gemini-only). Candidates trigger GitHub Actions via `repository_dispatch` for implementation.

| Monthly cost (daily triage, ~9 implementations) | Monthly cost (daily triage, 36 implementations) |
|------------------------------------------------|------------------------------------------------|
| **$0.66** | **$2.28** |

Triage adds only $0.12/month at daily frequency (30 × $0.004).

**Pros:** Best cost efficiency — triage runs frequently and cheaply, implementation only fires on real candidates. Separates scanning from execution.
**Cons:** Two systems to maintain. More complex orchestration (dispatch events, artifact passing). Debugging spans two platforms.

### Option 4: GitHub App / Webhook listener

A webhook receiver on Cloud Run, Lambda, or Cloudflare Workers listens for issue events and dispatches pipeline runs in real-time.

| Monthly cost (4 runs) | Monthly cost (36 runs) |
|----------------------|----------------------|
| **$0.24–$3.24** | **$2.26–$5.54** |

The wide range comes from the always-on receiver cost: free on Cloudflare Workers, up to ~$3/month for a min-instance Cloud Run service.

**Pros:** True real-time reactivity. Can filter events precisely.
**Cons:** Most complex to build. Requires webhook secret management, event validation, rate limiting. GitHub App registration overhead.

## Cost Summary

| Option | 4 runs/mo | 36 runs/mo | Complexity | Reactivity |
|--------|-----------|------------|------------|------------|
| **1. GitHub Actions** | **$0.24** | **$2.16** | **Low** | Cron + event |
| 2. Cloud Run | $0.24 | $2.31 | Medium | Cron only |
| 3. Hybrid | $0.66 | $2.28 | High | Cron + dispatch |
| 4. Webhook/App | $0.24–$3.24 | $2.26–$5.54 | Highest | Real-time |

**LLM cost dominates in every option.** At $0.06/run, the model calls account for 93–100% of total cost. Infrastructure is effectively free.

## Decision

Deploy the full pipeline as a **GitHub Actions workflow** (Option 1).

### Rationale

1. **Lowest complexity.** No GCP project, IAM, or container registry to manage. Secrets are native to GitHub.
2. **Lowest cost.** Tied for cheapest at every cadence, and free compute for public repos.
3. **Activity-driven.** Can trigger on `issues: labeled` with `agent:approved`, integrating directly with the existing HITL Gate 1 flow. No separate webhook infrastructure needed.
4. **Portable.** A GitHub Actions workflow is the most transferable artifact if we point the pipeline at a different repo in the future.
5. **Proven pattern.** The pipeline already shells out to `gh` CLI extensively; GitHub Actions is the natural execution environment for that.

### Implementation plan

1. Add `.github/workflows/implement.yml`:
   - Trigger: `schedule` (weekly, `0 9 * * 1`) + `workflow_dispatch` (manual) + `issues: labeled` (`agent:approved`)
   - Steps: checkout, Go setup, `make implement`
   - Secrets: `ANTHROPIC_API_KEY`, `GOOGLE_API_KEY`, `GH_TOKEN` (PAT with repo + PR scope)
2. Use `HITL_MODE=full` for scheduled runs, allow `workflow_dispatch` to override
3. Upload run artifacts (triage JSON, agent logs, cost summary) via `actions/upload-artifact`
4. Log per-run cost in the workflow summary using the existing cost tracker

### Graduation path

If triage needs to run more frequently than implementation (e.g., daily scanning, weekly implementing), graduate to Option 3 (hybrid) by:
- Keeping triage on Cloud Run per ADR 005 / issue #14
- Firing `repository_dispatch` to trigger the GitHub Actions implementer workflow
- This is additive — the GitHub Actions workflow stays the same, it just gains a new trigger source

## Consequences

### Positive
- Pipeline runs autonomously with zero infrastructure cost
- Activity-driven execution matches the "run whenever Conduit is active" goal
- Integrates with existing HITL labels without additional tooling
- Run artifacts are visible alongside PRs in the same platform

### Negative
- GitHub Actions has a 6-hour job timeout (our pipeline takes ~4 minutes, so this is not a practical concern)
- Cross-repo PR creation requires a PAT or GitHub App token (already required for local runs)
- Workflow logs are less searchable than a dedicated observability stack

### Mitigations
- The 6-hour timeout is 90× our actual runtime; if the pipeline grows, we can split into separate jobs
- PAT management is a known cost; if we later adopt a GitHub App (Option 4), the workflow just swaps the token source
- Run artifacts + cost summary in workflow output provide sufficient observability for the current scale

## Dashboard: GitHub Pages (Static)

Once the pipeline runs autonomously, we need visibility into run history, costs, and outcomes. Two options were evaluated:

| Option | Monthly cost | Capabilities |
|--------|-------------|-------------|
| **GitHub Pages (static HTML/JS)** | **$0** | Run history, cost charts, PR outcomes — reads from workflow artifacts via GitHub API |
| Cloud Run (served web app) | $0.01–$3.41 | Same + server-side filtering, auth, real-time websocket |

**Decision:** Start with GitHub Pages. The pipeline already produces structured `run.json` and `cost.json` artifacts. A static page reading aggregated JSON is sufficient for the current scale and the presentation. Graduate to Cloud Run only if we need server-side features (hundreds of runs, auth, real-time updates).

See issue #29 for implementation details.

## References

- Issue #28 — Deploy full pipeline for scheduled/reactive execution
- Issue #29 — Add pipeline dashboard (GitHub Pages)
- Issue #14 — Deploy triage agent to Cloud Run (triage-only, predecessor)
- ADR 005 — Triage agent Cloud Run deployment
- [Full pipeline achievement](../experiments/2026-04-07-full-pipeline-achievement.md) — Cost benchmarks
