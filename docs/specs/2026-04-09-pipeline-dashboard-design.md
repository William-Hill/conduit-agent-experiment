# Pipeline Dashboard Design

**Issue:** #29
**Date:** 2026-04-09
**Status:** Accepted

## Overview

A static GitHub Pages dashboard for monitoring the conduit-agent-experiment pipeline. Displays run history, cost trends, token usage, and PR outcomes. Supports triggering new runs and showing live status of active runs. Dual-purpose: presentation-ready visuals and daily operational monitoring.

## Design Decisions

- **Static site on GitHub Pages** — $0/month hosting, no backend, no database
- **Single HTML file** — vanilla JS, Chart.js from CDN, no build step or framework
- **Dark theme** — single-page stacked layout, scroll to explore
- **GitHub API for live features** — trigger runs and poll status via authenticated API calls
- **Token in localStorage** — optional GitHub PAT stored client-side for trigger/status features; without it, dashboard is read-only

## Data Source

The GitHub Actions workflow (`implement.yml`) uploads a `run-summary.json` artifact after each pipeline run. A separate GitHub Actions workflow (`aggregate-dashboard.yml`) collects all artifacts and writes a consolidated `docs/dashboard/data.json` file.

### Artifact schema (from CI run 24167191811)

```json
{
  "budget_exceeded": false,
  "cache_creation_tokens": 8582,
  "cache_read_tokens": 128730,
  "estimated_cost_usd": 0.281017,
  "input_tokens": 205097,
  "issue_number": 576,
  "issue_title": "Error codes needs to be documented in Swagger",
  "iterations": 15,
  "model": "claude-haiku-4-5-20251001",
  "output_tokens": 15184,
  "plan_chars": 24215,
  "pr_url": "https://github.com/ConduitIO/conduit/pull/2455",
  "summary": "...",
  "timestamp": "2026-04-09T01:20:01Z"
}
```

### Aggregated data format (`data.json`)

```json
{
  "runs": [
    { ...run-summary fields above, plus "run_id": "24167191811", "status": "success", "duration_seconds": 464 }
  ],
  "updated_at": "2026-04-09T01:30:00Z"
}
```

The `status` and `duration_seconds` fields are derived from the GitHub Actions run metadata during aggregation.

## Sections (top to bottom)

### 1. Header

- Project name: "conduit-agent-experiment"
- Subtitle: "Autonomous pipeline dashboard"
- Last updated timestamp from `data.json`
- Settings gear icon (opens token configuration modal)

### 2. Overview KPIs

Five metric cards in a horizontal row:

| Metric | Source | Color |
|--------|--------|-------|
| Total Runs | `runs.length` | Green (#4ecca3) |
| Total Cost | `sum(estimated_cost_usd)` | Green |
| Success Rate | `success / total * 100` | Green/amber/red based on threshold |
| PRs Created | `count where pr_url exists` | Blue (#7aa2f7) |
| Avg Iterations | `avg(iterations)` | White |

### 3. Pipeline Control

Three states:

**Idle:** Shows "No active runs" with last run info. Right side has a trigger form:
- Issue number input (optional — empty uses top from triage)
- HITL mode dropdown (yolo / full)
- "Run Pipeline" button (calls `POST /repos/{owner}/{repo}/actions/workflows/implement.yml/dispatches`)
- Disabled / hidden if no token configured

**In progress:** Polls `GET /repos/{owner}/{repo}/actions/runs?status=in_progress` every 15 seconds.
- Amber pulsing indicator with elapsed time
- Pipeline step visualization: Clone → Archivist → Planner → Reviewer → Implementer → Push → Draft PR
- V1: shows overall workflow status (queued/in_progress/completed). Steps show as "running" once the "Run pipeline" step starts.
- "View logs" link to the GitHub Actions run page

**Completed:** Green indicator, cost and duration summary, direct "View PR" button linking to the created PR.

### 4. Cost per Run (Chart)

Bar chart (Chart.js) showing cost per run over time.
- X axis: run date
- Y axis: cost in USD
- Bar color: green for success, red for failure/budget-exceeded
- Horizontal dashed line at $0.06 showing the baseline cost
- Tooltip: issue number, cost, iterations

### 5. Token Usage (Latest Run)

Horizontal progress bars showing token distribution for the most recent run:
- Input tokens (blue #7aa2f7)
- Output tokens (purple #a78bfa)
- Cache read tokens (green #4ecca3)
- Cache creation tokens (amber #e8b931)

Each bar labeled with the count. Bars are proportional to the largest value.

### 6. Run History Table

Sortable table with columns:

| Column | Source |
|--------|--------|
| Date | `timestamp` formatted |
| Issue | `issue_number` + truncated `issue_title` |
| Status | Badge: green "success" / red "failed" / amber "budget exceeded" |
| Iterations | `iterations` |
| Cost | `estimated_cost_usd` formatted |
| Model | `model` (display name) |
| PR | Link to `pr_url` or "-" |

Default sort: newest first.

## Authentication

On first visit (or click of settings gear), a modal prompts:

```
GitHub Personal Access Token (optional)

Enables: trigger pipeline runs, view live status
Token is stored in your browser only — never sent to any server except api.github.com.

[paste token here]
[Save]  [Skip — read-only mode]
```

Token stored in `localStorage` under key `gh_token`. Used as `Authorization: Bearer {token}` header for GitHub API calls. Can be cleared from settings.

Without a token:
- KPIs, cost chart, token usage, run history all work (read from static `data.json`)
- Pipeline Control section shows "Configure token to enable" instead of trigger/status
- No API calls are made

## File Structure

```
docs/dashboard/
  index.html          # Single-file dashboard (HTML + CSS + JS inline)
  data.json           # Aggregated run data (updated by CI workflow)
.github/workflows/
  implement.yml       # Existing — uploads run-summary.json artifact
  aggregate-dashboard.yml  # New — collects artifacts, writes data.json, commits
```

## Aggregation Workflow

`aggregate-dashboard.yml` runs:
- After each `implement.yml` completion (via `workflow_run` trigger)
- Downloads all `pipeline-run-*` artifacts
- Enriches each with run metadata (status, duration) from the Actions API
- Merges into `docs/dashboard/data.json`
- Commits and pushes to main (triggers GitHub Pages rebuild)

## GitHub Pages Configuration

Enable GitHub Pages in repo settings:
- Source: Deploy from branch
- Branch: `main`
- Folder: `/docs/dashboard`

Dashboard URL: `https://william-hill.github.io/conduit-agent-experiment/`

## Color Palette

| Token | Hex | Usage |
|-------|-----|-------|
| Green | #4ecca3 | Success, primary accent, section labels |
| Blue | #7aa2f7 | Links, PR badges, input tokens |
| Purple | #a78bfa | Output tokens |
| Amber | #e8b931 | Warnings, in-progress, cache creation |
| Red | #f7768e | Failures |
| BG Dark | #0f0f1a | Page background |
| BG Card | #1a1a2e | Card/panel background |
| Border | #2a2a4a | Card borders |
| Text Primary | #e0e0e0 | Main text |
| Text Secondary | #888888 | Labels, metadata |
| Text Muted | #555555 | Disabled, placeholder |

## Scope Boundaries

**In scope:**
- Static dashboard with all 6 sections above
- Aggregation workflow to build data.json
- GitHub Pages deployment
- Token-based trigger and live status polling

**Out of scope (future):**
- Per-step live breakdown during runs (requires pipeline changes to emit step markers)
- Historical token usage charts (v1 shows latest run only)
- Multi-repo support
- User authentication beyond localStorage PAT
- Mobile-responsive layout (desktop-first for presentations)
