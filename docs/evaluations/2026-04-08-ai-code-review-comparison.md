# AI Code Review Tool Comparison: Greptile vs CodeRabbit vs Codex

**Date:** 2026-04-08
**Author:** William Hill
**Method:** Side-by-side review of production PRs + planned controlled experiment

---

## Tools Under Evaluation

| Tool | Version/Plan | Integration | Cost |
|------|-------------|-------------|------|
| **Greptile** | Cloud (OSS discount applied) | GitHub App | $30/seat/mo (free for OSS) |
| **CodeRabbit** | Pro ($24/seat/mo) | GitHub App | $24/seat/mo, unlimited reviews |
| **Codex** (ChatGPT) | Connector bot | GitHub App | Included with ChatGPT subscription |

## Evaluation Dimensions

1. **Detection quality** -- what real issues does each catch vs miss?
2. **False positive rate** -- how much noise/nitpicking wastes time?
3. **Actionability** -- are suggestions copy-pasteable fixes or vague advice?
4. **Security awareness** -- does it catch path traversal, injection, credential leaks?
5. **Codebase context** -- does it understand cross-file relationships or just review diffs in isolation?
6. **Response time** -- how fast does each post its review?
7. **Cost** -- price per review at current usage level

---

## Observation 1: PR #22 (feat: per-step cost tracking and budget controls)

**PR scope:** New `internal/cost/` package with pricing, budget enforcement, and reporting. Changes across 15+ files including `cmd/implementer/main.go`, `internal/orchestrator/workflow.go`, docs, and tests.

**Review timestamps:**
- Greptile: 2026-04-08T03:29:31Z
- Codex: 2026-04-08T03:30:52Z
- CodeRabbit: 2026-04-08T03:32:19Z

All three reviewed within ~3 minutes of PR creation.

### Findings by Tool

#### Greptile (4 comments: 2 P1, 2 P2)

| # | Sev | File | Finding | Category |
|---|-----|------|---------|----------|
| 1 | P1 | workflow.go:358 | `CheckStep("architect")` is dead code -- `LoadBudget()` never loads an architect cap | Logic bug |
| 2 | P1 | budget.go:27 | `PLANNER_MAX_COST` loaded but never enforced -- no `CheckStep("planner")` call exists | Logic bug |
| 3 | P2 | budget.go:75 | Malformed env var (e.g. `0.5usd`) silently returns 0, disabling budget | Error handling |
| 4 | P2 | report.go:58 | Mixed-model steps report only first model name seen | Data accuracy |

#### Codex (2 comments: 1 P1, 1 P2)

| # | Sev | File | Finding | Category |
|---|-----|------|---------|----------|
| 1 | P1 | workflow.go:174 | Implementer budget not re-checked after `GenerateFileContent` -- expensive calls bypass cap | Logic bug |
| 2 | P2 | main.go:141 | `BudgetExceeded` flag ignored -- partial output still creates draft PR | Logic bug |

#### CodeRabbit (7 actionable, 15 nitpicks)

| # | Sev | File | Finding | Category |
|---|-----|------|---------|----------|
| 1 | Major | dossier_test.go:38 | Potential panic: `t.Errorf` on empty slice then index `[0]` | Test safety |
| 2 | Minor | achievement.md:189 | "Cost tracking" still listed as missing from pipeline | Docs staleness |
| 3 | Minor | JOURNEY.md:144 | Issue #21 status should be "Closed" | Docs staleness |
| 4 | Minor | design.md:12 | Heading level jump h1 -> h3 | Markdown lint |
| 5 | Minor | design.md:65 | Docs say unknown models return 0.0 but impl uses expensive fallback | Docs accuracy |
| 6 | Minor | workflow.go:351 | Architect budget check is no-op (same finding as Greptile #1) | Logic bug |
| 7 | Minor | reviewer.go:41 | `NewContentFromText` role param misuse for system instructions | API usage |
| +15 | Nitpick | various | Style: `120*1e9` -> `time.Second`, naming, import order, etc. | Style |

### Analysis

#### Detection quality

**Greptile** found the deepest logic bugs -- the planner budget never being enforced and the silent env var failure both require **cross-file reasoning** (understanding that `LoadBudget()` populates a map, but no caller checks the "planner" key). These are real bugs that would cause budget controls to silently fail in production.

**Codex** found a unique flow-level bug -- the `BudgetExceeded` flag being returned but never checked by the caller. This requires understanding the contract between `RunAgent()` and its call site across packages.

**CodeRabbit** had the broadest coverage but shallower depth on logic bugs. Its unique finds were the test panic risk and the API misuse (`NewContentFromText` role parameter). It was the only tool to catch documentation inconsistencies.

#### Overlap

Only **one finding** was shared: Greptile #1 and CodeRabbit #6 both identified the architect budget check as dead code. They arrived at it from different angles -- Greptile traced the `LoadBudget()` map population, CodeRabbit noted the missing env var.

**Zero overlap** between Codex and either other tool.

#### False positive rate

- **Greptile:** 0/4 -- all findings were real issues
- **Codex:** 0/2 -- both findings were real issues
- **CodeRabbit:** ~2-3/22 -- some nitpicks were subjective (naming preferences), and the heading-level lint is debatable for a design doc

#### Actionability

- **Greptile:** High -- included specific code fixes with exact line references
- **Codex:** Medium -- described the problem clearly but less specific on the fix
- **CodeRabbit:** High -- included committable suggestions with diff blocks for most comments

#### Codebase context

- **Greptile:** Strong -- traced `LoadBudget()` -> `StepCaps` map -> `CheckStep()` callers across 3 files
- **Codex:** Moderate -- understood `RunAgent()` return value contract but limited cross-file analysis
- **CodeRabbit:** Moderate -- good at surface-level cross-referencing (docs vs code) but missed the deeper budget enforcement gaps

#### Response time

All three responded within 3 minutes. Greptile was fastest (03:29), Codex second (03:30), CodeRabbit third (03:32). Differences are negligible.

### Scorecard: PR #22

| Dimension | Greptile | Codex | CodeRabbit |
|-----------|---------|-------|------------|
| Detection quality | **A** | B+ | B |
| False positive rate | **A** | **A** | B+ |
| Actionability | **A** | B | **A** |
| Security awareness | N/A | N/A | N/A |
| Codebase context | **A** | B | B |
| Response time | A | A | A |
| Coverage breadth | B | C | **A** |

**Greptile** wins on depth. **CodeRabbit** wins on breadth. **Codex** found a unique bug neither caught. Running all three together gives the best coverage -- their findings were almost entirely non-overlapping.

---

## Planned: Controlled Experiment

A future experiment will create a PR with ~10 seeded issues across four categories:

- 2-3 **real bugs** (nil dereference, off-by-one, race condition)
- 2-3 **security issues** (path traversal, unsanitized input, credential in env)
- 2-3 **style/quality issues** (dead code, duplicate logic, missing error check)
- 1-2 **architectural issues** (wrong package boundary, breaking an interface contract)

Each tool's detection rate, false positives, and suggestion quality will be scored per category. This will provide a controlled baseline to complement the organic observations above.

See GitHub issue for tracking.

---

## Ongoing Observations

Future PRs will be tracked here as additional data points.

| PR | Date | Greptile findings | Codex findings | CodeRabbit findings | Notes |
|----|------|-------------------|----------------|---------------------|-------|
| #22 | 2026-04-08 | 4 (2P1, 2P2) | 2 (1P1, 1P2) | 7+15 nitpicks | First three-way comparison |
