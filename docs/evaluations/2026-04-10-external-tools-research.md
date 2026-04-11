# External Tools Research: Aider, goclaw, OpenRouter, mutant, nanobot

**Date:** 2026-04-10
**Author:** William Hill
**Source:** Suggestions from the original Conduit creator during a project review
**Method:** Documentation review, README analysis, and targeted web search
**Status:** Research only — no code changes yet. Prototype tracked in issue #38.

---

## Why this evaluation

The original Conduit creator endorsed the autonomous-maintenance pivot and pointed at five
tools that might push the project further toward two goals:

1. **More autonomous** — reduce the custom glue we're maintaining ourselves.
2. **Cheaper to run** — ideally reach $0/run on free tiers without sacrificing PR quality.

This doc captures what each tool actually does, how it would fit (or not) into our current
5-agent pipeline (triage → archivist → planner → reviewer → implementer), and which
experiments are worth running.

---

## Summary ranked by fit

| Rank | Tool | Fit | Action |
|------|------|-----|--------|
| 1 | **Aider + OpenRouter** | High | Prototype as implementer replacement (issue #38) |
| 2 | **Mutation testing** (concept from `mutant`, Go impl via Gremlins) | High | Add as post-implementer quality gate |
| 3 | **goclaw** | Reference only | Mine architectural patterns; do NOT depend on (CC BY-NC 4.0) |
| 4 | **nanobot** | Low | Skim memory design; no integration planned |
| 5 | **OpenRouter** (standalone) | Enabler for #1 | Ships with Aider prototype |

---

## 1. Aider — https://aider.chat/

**What it is:** Free/OSS terminal pair-programmer. 100+ languages, automatic git commits,
repo map, automatic linting + test runs after edits.

**Key capabilities relevant to us:**

- **Non-interactive mode is production-ready.**
  ```bash
  aider --message "<spec>" --yes --auto-commits file1.go file2.go
  ```
  Runs one task and exits — drop-in for our implementer stage. Source:
  https://aider.chat/docs/scripting.html
- **First-class OpenRouter support.** `aider --model openrouter/<provider>/<model>`,
  configured via `OPENROUTER_API_KEY`. Source:
  https://aider.chat/docs/llms/openrouter.html
- **Repo map grounds edits in real symbols.** Aider builds a ranked symbol index of the
  repo and feeds it as context. This is the most promising anti-hallucination mechanism
  we've seen — hallucinated symbols is the persistent failure mode across every experiment
  in `docs/experiments/`.
- **Python API exists but is unsupported.** Stable integration path is the CLI.

**How it would replace our current implementer:**

Today: `cmd/implementer/main.go` calls `anthropic-sdk-go`'s `BetaToolRunner` with 5 custom
tools (`read_file`, `write_file`, `list_dir`, `search_files`, `run_command`) against a
cloned target repo. See `docs/superpowers/plans/2026-04-06-implementer-agent.md`.

With Aider: planner emits a spec → we shell out to `aider --message "<spec>" --yes
<target files>` on the cloned repo → reviewer reads the resulting commit. We delete our
tool implementations and inherit Aider's repo map, auto-lint, and auto-test loop for free.

**Risks:**

- Aider's edit format is LLM-specific. Free OpenRouter models (Qwen3 Coder, DeepSeek R1)
  may not produce clean diff edits as reliably as Sonnet. Needs empirical measurement.
- We lose fine-grained control over tool call sequencing and intermediate state.
- Free-tier rate limits (see below) can bite mid-run.

---

## 2. OpenRouter — https://openrouter.ai/

**What it is:** OpenAI-compatible gateway to 300+ models across 60+ providers. Unified
billing, provider fallback, drop-in replacement for `OPENAI_*` env vars.

**The "free tier" story (April 2026):**

- **Free model rate limits:** 20 requests/minute, 200 requests/day, **per model**.
  Source: https://openrouter.ai/docs/api/reference/limits
- **Strongest free coding models:**
  - **Qwen3 Coder 480B (free)** — 262K context, state-of-the-art free code gen
  - **DeepSeek R1 (free)** — reasoning-heavy, strong on debugging and planning
  - **Llama 3.3 70B Instruct (free)** — general-purpose fallback
  - Source: https://costgoat.com/pricing/openrouter-free-models
- **Rate-limit mitigation:** each free model has its own quota, so round-robining across
  model IDs multiplies effective throughput.

**Proposed per-stage model mapping for $0.01/run pipeline:**

| Stage | Today | With OpenRouter free tier |
|-------|-------|---------------------------|
| Triage | Haiku ($) | DeepSeek R1 (free) |
| Archivist | Haiku ($) | DeepSeek R1 (free) |
| Planner | Sonnet ($$) | DeepSeek R1 (free) |
| Implementer | Sonnet ($$$) via custom loop | Qwen3 Coder 480B (free) via Aider |
| Reviewer | Sonnet ($$) | **Stay on Sonnet** — quality gate that matters |

200/day × ~4 models = enough headroom for current pipeline volume. Reviewer stays on paid
Sonnet because it's the final quality gate; the ~$0.02/run is the cost we should keep.

**Risks:**

- Free models have variable latency and can silently rate-limit mid-pipeline. Need retry
  with cross-model fallback — OpenRouter's provider routing handles some of this.
- Quality regression is likely on edge cases. A/B measurement is essential before
  switching the live pipeline.

---

## 3. goclaw — https://github.com/nextlevelbuilder/goclaw

**What it is:** Multi-tenant AI agent gateway in Go. 20+ providers (incl. OpenRouter),
8-stage pipeline (context → history → prompt → think → act → observe → memory →
summarize), 3-tier memory (working → episodic → semantic), agent teams, cron, sandbox,
single 25 MB binary.

**Honest assessment:** Architecturally it is almost a superset of what we're building. It
already ships:

- Cron scheduling (our issue #28 territory)
- Encrypted per-user API keys (onboarding work from issue #20)
- Prompt caching, OpenRouter adapter, sandboxed exec, built-in
  `edit_file`/`search`/`glob`
- Self-evolution loop with guardrails — directly relevant to our agent quality problem

**The blocker: license.** goclaw is **CC BY-NC 4.0 (non-commercial only).** If this
project ever ships as a Meroxa service or inside a commercial Conduit offering, we cannot
depend on goclaw. It is off the table as a runtime dependency.

**What it's good for:** a reference implementation to mine for patterns. Specifically
worth reading:

- The 8-stage pipeline decomposition — especially the `observe` and `memory` stages,
  which map onto our review-feedback loop (`docs/plans/2026-04-08-review-feedback-loop.md`)
- The 3-tier memory architecture — may inform our archivist agent
  (`docs/superpowers/plans/2026-04-07-archivist-agent.md`)

**Recommendation:** Skim their docs for ideas, do not import.

---

## 4. mutant — https://github.com/mbj/mutant

**What it is:** Mutation testing for Ruby. The README's pitch is almost verbatim our
problem statement:

> AI writes your code. AI writes your tests. But who tests the tests?
> Passing tests aren't the same as *meaningful* tests.

**Why this is the most interesting idea in the whole list:**

Our persistent failure mode per `docs/experiments/` and
`~/.claude/.../memory/project_agent_failure_modes.md` is **hallucinated symbols** and the
associated shallow tests — the implementer writes tests that pass against code that
references things that don't exist, or tests that don't actually exercise the behavior
they claim to.

Mutation testing is the natural defense: perturb the generated code, then assert the
generated tests *catch* the perturbation. A test suite that passes against hallucinated
behavior almost always fails a mutation run because the tests never actually exercised
the hallucinated paths.

**For Go** (mutant is Ruby-only), the available tools are:

| Tool | Status | Notes |
|------|--------|-------|
| [Gremlins](https://github.com/go-gremlins/gremlins) | Active | Designed as a CI quality gate; best fit |
| [go-mutesting (avito-tech fork)](https://github.com/avito-tech/go-mutesting) | Active | Original, forked and maintained |
| [Ooze](https://github.com/gtramontina/ooze) | Active | MIT, newer |

**Proposed integration — post-implementer gate:**

```
implementer → go test → Gremlins (diffed files only) → reviewer
                              ↓
                       if mutation score < threshold:
                          kick back to implementer with
                          surviving mutants as feedback
```

Benefits:

- Directly attacks the "tests pass but nothing is actually verified" failure class
- Feeds the reviewer only code that survived mutation testing (higher signal)
- **Genuinely novel** — none of our tracked competitors (SWE-agent, OpenHands, Copilot
  Agent, Sweep, Devin) do this. Potential differentiator.

**Cost:** Mutation testing is slow — runs the test suite N times. Mitigate by scoping to
changed files only. Acceptable in the agent pipeline, not in the dev inner loop.

**Follow-up:** Track as a separate issue after the Aider prototype lands.

---

## 5. nanobot — https://github.com/HKUDS/nanobot

**What it is:** 38k⭐ Python "ultra-lightweight personal AI agent." MIT. OpenRouter /
OpenAI-compat / MCP support, Python SDK, 2-stage "Dream" memory, skills, cron, sandbox,
Anthropic prompt caching. Aimed at Telegram/Discord chat UX.

**Honest read:** nanobot is a personal assistant framework, not a headless
code-modification pipeline. Feature overlap with our existing Go pipeline is high and
strategic fit for "maintain an OSS Go repo from GitHub issues" is low.

**The one interesting piece:** their 2-stage Dream memory design for long-running agents.
May inform the archivist agent's repo-history summarization. Worth 15 minutes of reading,
not a dependency candidate.

**Recommendation:** Skim. No integration work.

---

## What happens next

| # | Action | Owner | Tracking |
|---|--------|-------|----------|
| 1 | Prototype Aider + OpenRouter free tier as implementer; A/B against current Sonnet+BetaToolRunner on seeded issues | @William-Hill | Issue #38 |
| 2 | Add mutation testing (Gremlins) as post-implementer quality gate | @William-Hill | Separate issue after #38 lands |
| 3 | Read goclaw's 8-stage pipeline + 3-tier memory for ideas applicable to review feedback loop and archivist | @William-Hill | Background reading |
| 4 | Skim nanobot Dream memory | @William-Hill | Background reading |

---

## Prototype #1 results (pending — tracked in #38)

**Status:** Infrastructure landed. Experiment runs not yet executed.

Once the runs are complete, fill in:

- Cost per arm (mean ± stdev)
- Success rate (builds, tests, internal reviewer pass)
- Mean hallucinated symbol count per arm
- Mean wall-clock time per run
- OpenRouter rate-limit events observed
- Qualitative notes: edit-format issues, spec compatibility, surprising behaviors

Run the experiment with:

```bash
./scripts/ab-experiment.sh 3
go run ./cmd/ab-analyze data/ab-runs > docs/evaluations/ab-results-raw.txt
```

Then paste the analyzer table here and write the recommendation (adopt /
reject / hybrid).

---

## Sources

- Aider: https://aider.chat/, https://aider.chat/docs/scripting.html, https://aider.chat/docs/llms/openrouter.html
- goclaw: https://github.com/nextlevelbuilder/goclaw, https://docs.goclaw.sh
- OpenRouter: https://openrouter.ai/, https://openrouter.ai/docs/api/reference/limits, https://costgoat.com/pricing/openrouter-free-models
- mutant: https://github.com/mbj/mutant
- Go mutation testing: https://github.com/go-gremlins/gremlins, https://github.com/avito-tech/go-mutesting, https://github.com/gtramontina/ooze
- nanobot: https://github.com/HKUDS/nanobot, https://nanobot.wiki
