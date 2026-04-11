# Onboarding Guide

Get the pipeline running end-to-end in under 10 minutes.

## Prerequisites

- **Go 1.25+** (`go version`)
- **gh CLI** installed and authenticated (`gh auth status`)
- **API keys:**
  - `ANTHROPIC_API_KEY` — [console.anthropic.com](https://console.anthropic.com/) (for the implementer agent)
  - `GOOGLE_API_KEY` or `GEMINI_API_KEY` — [aistudio.google.com](https://aistudio.google.com/) (for archivist, planner, reviewer)
- **A GitHub fork** of the target repo with push access (default: `William-Hill/conduit`)

## Setup

### 1. Clone and enter the repo

```bash
git clone https://github.com/William-Hill/conduit-agent-experiment.git
cd conduit-agent-experiment
```

### 2. Create your `.env` file

```bash
cp .env.example .env
# Edit .env with your keys:
#   ANTHROPIC_API_KEY=sk-ant-...
#   GEMINI_API_KEY=AIza...
```

Then load it:

```bash
set -a && source .env && set +a
```

### 3. Verify the build

```bash
go build ./...
```

## First Run

The fastest way to see the pipeline work:

```bash
HITL_MODE=yolo IMPL_ISSUE_NUMBER=576 make implement
```

This runs in **yolo mode** (no human approval gates) on a safe documentation issue. It will:

1. Fetch issue #576 from ConduitIO/conduit
2. Clone the repo to a temp directory
3. Run archivist (Gemini Flash explores the repo, ~16s)
4. Run planner (Gemini Flash writes an implementation plan, ~1.5 min)
5. Run reviewer (Gemini Flash validates the plan, ~14s)
6. Run implementer (Claude Haiku writes code iteratively, ~2 min)
7. Push a branch and open a draft PR

Total: ~4 minutes, ~$0.06-0.28.

### What to watch in the output

| Log line | What's happening |
|----------|-----------------|
| `Archivist found N relevant files` | Gemini explored the repo |
| `Plan produced (N chars)` | Implementation plan generated |
| `Plan approved` | Reviewer validated the plan |
| `[iter N] tool: read_file/write_file` | Claude is writing code |
| `Agent completed in N iterations` | Code generation done |
| `Draft PR created: URL` | PR is live on GitHub |

### Safe demo issues

These are low-risk documentation tasks, good for a first run:

| Issue | Title | Type |
|-------|-------|------|
| #576 | Error codes needs to be documented in Swagger | docs |
| #1268 | Write a guide about embedding Conduit | docs |
| #1855 | Write a guide for setting up a pipeline | docs |

## Running via GitHub Actions

The pipeline also runs autonomously in CI:

**Manual trigger:**
```bash
gh workflow run implement.yml -f hitl_mode=yolo -f issue_number=576
gh run watch
```

**Scheduled:** Runs every Monday at 9am UTC automatically.

**Dashboard:** View run history, costs, and trigger runs at the [pipeline dashboard](https://william-hill.github.io/conduit-agent-experiment/dashboard/).

## Pointing at a Different Repo

To target a different GitHub repository:

```bash
export IMPL_REPO_OWNER=your-org       # Target repo owner
export IMPL_REPO_NAME=your-repo       # Target repo name
export IMPL_FORK_OWNER=your-username  # Your fork (where branches are pushed)
```

The pipeline needs:
- The target repo to be public (or your `gh` CLI authenticated with access)
- A fork under `IMPL_FORK_OWNER` with push access
- Open issues on the target repo (or use `IMPL_ISSUE_NUMBER` to pick one)

## Environment Variables Reference

### Required

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Claude API key (implementer, responder) |
| `GOOGLE_API_KEY` or `GEMINI_API_KEY` | Gemini API key (archivist, planner, reviewer, triage) |

### Pipeline (IMPL_*)

| Variable | Default | Description |
|----------|---------|-------------|
| `IMPL_REPO_OWNER` | `ConduitIO` | Target repo owner |
| `IMPL_REPO_NAME` | `conduit` | Target repo name |
| `IMPL_FORK_OWNER` | `William-Hill` | Fork to push branches to |
| `IMPL_TRIAGE_DIR` | `data/tasks` | Directory with triage JSON output |
| `IMPL_ISSUE_NUMBER` | (auto) | Override: pick a specific issue |
| `IMPL_MODEL` | Haiku 4.5 | Anthropic model for implementer |
| `IMPL_MAX_COST` | (none) | Budget cap in USD |

### HITL (Human-in-the-Loop)

| Variable | Default | Description |
|----------|---------|-------------|
| `HITL_MODE` | `full` | `full` (approval gates), `yolo` (autonomous), `custom` |
| `HITL_GATE1_POLL_INTERVAL` | `5m` | How often to check for approval labels |
| `HITL_GATE3_POLL_INTERVAL` | `5m` | How often to check for PR actions |
| `HITL_BOT_REVIEW_WAIT` | `120s` | Wait time for bot reviews |
| `HITL_BOT_MAX_ITERATIONS` | `3` | Max bot review/fix cycles |
| `HITL_BOT_REVIEWERS` | `@coderabbitai review,@greptile review` | Bot trigger comments |

### Responder

| Variable | Default | Description |
|----------|---------|-------------|
| `RESPONDER_PR_NUMBER` | (required) | PR to address review comments on |
| `RESPONDER_MAX_ITERATIONS` | `3` | Max fix/review cycles |
| `RESPONDER_WAIT_SECONDS` | `120` | Wait between iterations |
| `RESPONDER_MODEL` | Haiku 4.5 | Model for fix agent |

### Triage

| Variable | Default | Description |
|----------|---------|-------------|
| `TRIAGE_REPO_OWNER` | `ConduitIO` | Repository owner for triage |
| `TRIAGE_REPO_NAME` | `conduit` | Repository name for triage |
| `TRIAGE_OUTPUT_DIR` | `data/tasks` | Output directory for triage results |

## Cost

| Cadence | Runs/month | Monthly cost |
|---------|-----------|-------------|
| Weekly | 4 | $0.24 |
| Match Conduit active pace | 36 | $2.16 |
| Daily | 30 | $1.80 |

Per-run cost: ~$0.06 (with prompt caching). See [ADR 006](adr/006-pipeline-deployment-github-actions.md) for the full cost analysis.

## Optional: Aider + OpenRouter backend (experimental, issue #38)

The implementer supports a second backend that shells out to the
[Aider](https://aider.chat/) CLI and routes through
[OpenRouter](https://openrouter.ai/) — typically against a free-tier model
such as Qwen3 Coder. This is the experimental arm for the A/B prototype
tracked in issue #38; it is not yet the default.

### Install Aider

```bash
# Preferred: pipx (isolated, no global package pollution)
brew install pipx   # or: python3 -m pip install --user pipx
pipx install aider-chat

# Verify
aider --version
```

### Create an OpenRouter account and key

1. Sign up at https://openrouter.ai/
2. Create an API key under https://openrouter.ai/keys
3. Export it alongside your other API keys:

```bash
export OPENROUTER_API_KEY="sk-or-v1-…"
```

### Run the pipeline against the Aider backend

```bash
IMPL_BACKEND=aider \
IMPL_AIDER_MODEL="openrouter/qwen/qwen-2.5-coder-32b-instruct:free" \
OPENROUTER_API_KEY="sk-or-v1-…" \
  go run ./cmd/implementer
```

`IMPL_AIDER_MODEL` is optional; the backend defaults to a free-tier Qwen
Coder model. Setting it to an explicit empty string (`IMPL_AIDER_MODEL=`)
is treated the same as unset and yields the default. Other useful
free-tier models:

- `openrouter/deepseek/deepseek-r1:free`
- `openrouter/meta-llama/llama-3.3-70b-instruct:free`

Note: when running the Aider backend, only `OPENROUTER_API_KEY` is
required — the `ANTHROPIC_API_KEY` check is scoped to the anthropic
backend so Aider-only experiments don't need an Anthropic key.

### Run the A/B experiment

```bash
./scripts/ab-experiment.sh 3     # 3 iterations per task per backend
go run ./cmd/ab-analyze data/ab-runs
```

### Rate limits

OpenRouter's free tier is capped at **20 requests/minute, 200 requests/day
per model**. The AiderBackend does not retry or round-robin across models —
if you hit the cap, wait or switch `IMPL_AIDER_MODEL` to a different free
model for the next run.

## Troubleshooting

**"no triage files found"** — Either run `make triage` first, or use `IMPL_ISSUE_NUMBER=576` to bypass triage.

**Push rejected (branch exists)** — Delete the stale branch:
```bash
gh api repos/William-Hill/conduit/git/refs/heads/agent/fix-576 -X DELETE
```

**Agent produces no changes** — The implementer hit its 15-iteration limit without finishing. Usually means the plan was too ambitious. Try a simpler issue.

**Budget exceeded** — Set `IMPL_MAX_COST=0.50` to cap spending. The pipeline halts before PR creation if exceeded.
