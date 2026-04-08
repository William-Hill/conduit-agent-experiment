# PR Reviewer Comparison Round 2: Greptile vs CodeRabbit

**Date:** 2026-04-08
**PR:** #27 (HITL integration points)
**Previous experiment:** `2026-04-07-pr-reviewer-comparison.md` (PR #22, cost tracking)

## Overview

PR #27 added human-in-the-loop gates to the agent pipeline — a new `internal/hitl/` package, GitHub adapter extensions, and CLI integration across 16 files. This is the second time we've compared automated reviewer output on the same PR, building on the patterns observed in PR #22.

## Summary of Findings

| Capability | Greptile | CodeRabbit |
|-----------|----------|------------|
| Cross-file logic bugs | **Strong** (fork push mismatch) | Not caught |
| Semantic correctness (name vs behavior) | **Strong** (ResolveAddressedThreads) | Not caught |
| Robustness / error handling | Yes (transient error abort) | Yes (same finding) |
| Code duplication detection | Yes (fetchPRComments) | Not flagged |
| Defensive coding | Yes (extractPRNumber) | Yes (nil check, CombinedOutput) |
| Test isolation | Not flagged | Yes |
| Documentation gaps | Not flagged | Yes (100-thread limit) |
| Variable shadowing | Not flagged | Yes |
| Committable fix suggestions | No (describes fix) | Yes (exact diffs) |

## Detailed Findings

### Greptile: 3 P1s, 2 P2s

**P1: Hardcoded `origin` remote breaks fork push in Gate 3 loop**
Greptile traced the push logic from `CreateBranchAndPush` (which checks `ForkOwner != Owner` and uses the `"fork"` remote) to the Gate 3 bot review loop (which hardcoded `"origin"`). With default config (`ForkOwner=William-Hill`, `Owner=ConduitIO`), the initial PR push goes to `fork` but subsequent fix pushes go to `origin` — a real bug that would fail at runtime. This required understanding code flow across `internal/github/adapter.go` and `cmd/implementer/main.go`. **CodeRabbit did not catch this.**

**P1: `ResolveAddressedThreads` resolves ALL threads, not just addressed ones**
Greptile identified a semantic mismatch: the function name implies it only resolves threads the agent addressed, but it actually resolves every unresolved thread — including human reviewer comments the agent never touched. This could hide important feedback from the human approver. **CodeRabbit did not catch this.**

**P1: Transient API errors immediately abort polling loop**
Both reviewers independently flagged that a single network hiccup would kill a gate that might have been waiting hours. Both suggested retry tolerance with consecutive error tracking.

**P2: `fetchPRComments` bypasses adapter and duplicates code**
Greptile noted the function shells out to `gh` directly instead of using the adapter's `runGH` method, and that it's copy-pasted identically in `cmd/responder/main.go`.

**P2: `extractPRNumber` fragile for trailing slashes**
Defensive coding issue — URLs with trailing slashes would yield empty string from `Split`.

### CodeRabbit: 2 Actionable, 4 Nitpicks

**Actionable: `CombinedOutput` may corrupt JSON**
CodeRabbit caught that `fetchPRComments` uses `CombinedOutput` which mixes stdout and stderr — if `gh` emits rate limit warnings, they'd be prepended to JSON and break parsing. Provided a committable diff fixing it. **Greptile flagged the same function but focused on adapter bypass, not the stdout/stderr issue.**

**Actionable: Polling loop fails on first transient error**
Same finding as Greptile P1 — provided a concrete `consecutiveErrors` counter implementation.

**Nitpick: Nil check in HITLAdapter.GetPRState**
Defensive guard against `(nil, nil)` return from underlying adapter.

**Nitpick: Test isolation for defaults test**
`TestLoadConfig_Defaults` assumes clean env — suggested `t.Setenv` to clear vars explicitly.

**Nitpick: 100 review thread limit undocumented**
GraphQL query has `first: 100` with no pagination — suggested documenting the limit.

**Nitpick: Variable shadowing**
`statusCmd`/`statusOutput` redeclared in Gate 3 loop, shadowing outer declarations.

## Cross-PR Pattern Consolidation

Combining observations from PR #22 and PR #27:

### Greptile's Core Strength: Cross-File Semantic Analysis

| PR | Finding | Pattern |
|----|---------|---------|
| #22 | `CheckStep("architect")` dead code | Traced call site → function → map → config loader across 4 files |
| #22 | `PLANNER_MAX_COST` loaded but never enforced | Searched all `CheckStep` call sites, confirmed none pass "planner" |
| #22 | Cache tokens excluded from budget calc | Understood Anthropic's 3-tier caching pricing and traced through budget calculation |
| #27 | Hardcoded `origin` breaks fork push | Compared push logic in `CreateBranchAndPush` with Gate 3 loop across 2 files |
| #27 | `ResolveAddressedThreads` resolves all threads | Name-vs-behavior mismatch with user-facing impact |

**Pattern:** Greptile iteratively searches across files to trace data flow and identify semantic breaks — dead code, unused config, logic mismatches between components. This is the same strategy a human reviewer uses (`grep -rn` chains) but automated.

### CodeRabbit's Core Strength: Local Correctness + Documentation

| PR | Finding | Pattern |
|----|---------|---------|
| #22 | Spec doc says "Unknown models return $0" but code changed | Documentation drift detection |
| #22 | `dossier_test.go` potential index panic | Test safety (non-fatal assertion before array access) |
| #22 | `genai.NewContentFromText` role misuse | Web research for API correctness |
| #27 | `CombinedOutput` corrupts JSON with stderr | Local function correctness |
| #27 | Test isolation assumes clean env | Test robustness |
| #27 | 100-thread pagination limit undocumented | Documentation gap |

**Pattern:** CodeRabbit excels at AST/pattern-based analysis within single files — catching well-known anti-patterns (CombinedOutput, test isolation, nil checks, variable shadowing), documentation inconsistencies, and API misuse. It also provides committable fix suggestions with exact diffs, which speeds up remediation.

### Overlap Zone

Both reviewers catch robustness/error-handling issues (transient error tolerance, resource leaks). When they overlap, Greptile tends to give better *why* reasoning while CodeRabbit gives better *how to fix* diffs.

### Codex Connector (PR #22 only, not available for PR #27)

Codex found runtime ordering bugs (budget check placement, flag ignored in CLI) that neither Greptile nor CodeRabbit caught. Without Codex data on PR #27, we can't confirm whether this gap persists. Codex hit usage limits on PR #27.

## Recommendation

Run all available reviewers in parallel. Total cost is minimal (free tiers or low-cost plans), total latency is under 2 minutes, and no single reviewer catches everything:

| Reviewer | Run For |
|----------|---------|
| Greptile | Cross-file logic bugs, semantic correctness, data flow tracing |
| CodeRabbit | Local correctness, documentation, test safety, committable fixes |
| Codex | Runtime ordering, control flow (when available) |

## Key Takeaway

After two PRs, the pattern is clear: **Greptile finds bugs humans would find with grep chains, CodeRabbit finds bugs linters would find with better rules.** Both are valuable, and neither replaces the other.
