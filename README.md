# conduit-agent-experiment

A multi-agent system that autonomously triages, plans, and implements fixes for open source repositories. Built as an experiment in AI-assisted maintenance, targeting [ConduitIO/conduit](https://github.com/ConduitIO/conduit) as the test subject.

## What it does

Given a GitHub repository, this system:

1. **Triages** open issues using an ADK Go agent with Gemini Flash, classifying by type, difficulty, and demand
2. **Researches** the codebase via an archivist that greps for relevant files and builds a dossier
3. **Plans** exact code changes in a detailed Markdown implementation document
4. **Reviews** the plan against the dossier to catch hallucinated symbols and incorrect paths
5. **Implements** the plan using Claude with iterative tool use (read, write, search, build)
6. **Opens a draft PR** with the changes

Total cost per run: **~$0.06**. Total time: **~3 minutes**.

## Architecture

```text
triage (ADK Go + Gemini Flash)
    -> archivist (Go + Gemini Flash, single call)
    -> planner (Gemini Flash, Markdown output)
    -> reviewer (Gemini Flash, JSON approve/reject)
    -> implementer (anthropic-sdk-go + Claude Haiku 4.5, 15 iterations)
    -> draft PR (gh CLI)
```

The key insight: **cheap models think, expensive models write**. Gemini Flash handles 4 of 5 pipeline steps at 1/20th the cost. Claude handles the mechanical code writing with iterative compile-check loops.

## Quick start

### Prerequisites

- Go 1.25+
- `gh` CLI (authenticated)
- API keys: `ANTHROPIC_API_KEY`, `GOOGLE_API_KEY` (or `GEMINI_API_KEY`)

### Run the triage agent

```bash
export GOOGLE_API_KEY=your-key
make triage
```

### Run the full pipeline

```bash
export ANTHROPIC_API_KEY=your-key
export GOOGLE_API_KEY=your-key
make implement
```

### Configuration

Environment variables for the implementer:

| Variable | Default | Description |
|----------|---------|-------------|
| `IMPL_REPO_OWNER` | `ConduitIO` | Target repo owner |
| `IMPL_REPO_NAME` | `conduit` | Target repo name |
| `IMPL_FORK_OWNER` | `William-Hill` | Fork to push branches to |
| `IMPL_TRIAGE_DIR` | `data/tasks` | Directory with triage JSON output |
| `IMPL_ISSUE_NUMBER` | (auto) | Override: pick a specific issue |
| `IMPL_MODEL` | (Haiku 4.5) | Anthropic model for implementer |

## Project structure

```text
cmd/
  triage/          # ADK-based issue triage agent
  implementer/     # Full pipeline: triage -> archivist -> planner -> reviewer -> implementer -> PR
  experiment/      # Original orchestration CLI (milestone 1-2)
internal/
  triage/          # Issue classification and ranking
  archivist/       # Repo exploration + dossier generation
  planner/         # Implementation plan + review
  implementer/     # Claude-based code writer with 5 tools
  llmutil/         # Shared LLM utilities
  github/          # GitHub adapter (gh CLI wrapper)
  ...
docs/
  design.md        # Full PRD and architecture
  experiments/     # Structured experiment reports
  specs/           # Design specs and implementation plans
  adr/             # Architecture decision records
data/
  tasks/           # Triage output and task definitions
  runs/            # Historical run artifacts
configs/           # YAML configuration
```

## CI / Automated Runs

The pipeline can run autonomously via GitHub Actions (see [ADR 006](docs/adr/006-pipeline-deployment-github-actions.md)):

- **Weekly cron:** Runs every Monday at 9am UTC
- **Manual dispatch:** Trigger from the Actions tab with optional issue number, HITL mode, and model override
- **Event-driven:** Responds to `repository_dispatch` events for integration with external triggers

### Required secrets

| Secret | Description |
|--------|-------------|
| `ANTHROPIC_API_KEY` | Claude API key (implementer) |
| `GOOGLE_API_KEY` | Gemini API key (archivist, planner, reviewer) |
| `GH_TOKEN` | GitHub PAT with `repo` scope (cross-repo PR creation) |

### Cost

~$0.06/run. Matching Conduit's active-period velocity (36 runs/month) costs ~$2.16/month. See the [full cost analysis](docs/adr/006-pipeline-deployment-github-actions.md#cost-summary).

## Tests

```bash
make test
```

## Documentation

- **[Onboarding Guide](docs/onboarding.md)** -- get the pipeline running in under 10 minutes
- **[Cost Analysis](docs/cost-analysis.md)** -- canonical reference for per-run cost, deployment options, and projected feature impact
- **[Demo Guide](docs/demo-guide.md)** -- step-by-step instructions for running the pipeline end-to-end
- **[Experiment Journey](docs/JOURNEY.md)** -- detailed history of what we built, what worked, what failed, and where the project is headed
- **[Experiments](docs/experiments/)** -- structured reports from each pipeline run
- **[Design](docs/design.md)** -- full PRD, hypotheses, architecture, and cost model
- **[ADRs](docs/adr/)** -- architecture decision records
- **[Pipeline Dashboard](https://william-hill.github.io/conduit-agent-experiment/)** -- live run history, cost trends, and pipeline control

## Status

The pipeline is functional end-to-end with human-in-the-loop gates, automated code review integration, and cost tracking. Three operating modes: `full` (production, with approval gates), `yolo` (demo, fully autonomous), `custom` (per-gate control).

See [open issues](https://github.com/William-Hill/conduit-agent-experiment/issues) for what's next.

## License

This is a research project. The target repository (ConduitIO/conduit) has its own license.
