# PR Reviewer Comparison: Greptile vs CodeRabbit vs Codex Connector

**Date:** 2026-04-07
**PR:** #22 (Per-step cost tracking and budget controls)

## Overview

PR #22 received automated reviews from three different bots: Greptile, CodeRabbit, and ChatGPT Codex Connector. This document compares their review quality, unique capabilities, and blind spots based on the same PR.

## Summary of Findings

| Capability | Greptile | CodeRabbit | Codex Connector |
|-----------|----------|------------|-----------------|
| Cross-file logic analysis | Strong | Moderate | Strong |
| Dead code / unreachable path detection | Strong (P1) | Yes (duplicated Greptile) | No |
| Control flow tracing (grep loops) | Yes | No | Partial |
| Documentation consistency | No | Strong | No |
| Static analysis (linting, markdown) | No | Yes (MD001, etc.) | No |
| Test safety (panic guards) | No | Yes (Major) | No |
| Actionable code suggestions | Yes (inline) | Yes (committable) | Yes (inline) |
| Web research for API correctness | No | Yes (genai SDK) | No |
| Priority labeling | P1/P2 badges | Major/Minor | P1/P2 badges |

## Detailed Observations

### Greptile: Best at cross-file logic tracing

Greptile's standout capability is **grep-loop analysis** — tracing how a value flows across files and identifying when that flow is broken. Two P1 findings demonstrated this:

1. **`CheckStep("architect")` is dead code**: Greptile traced from the `workflow.go` call site → into `budget.go:CheckStep` → through the `StepCaps` map → back to `LoadBudget()` → confirmed no `ARCHITECT_MAX_COST` env var is loaded. This required understanding four files and the data flow between them. CodeRabbit also caught this, but Greptile's explanation was more precise about *why* it's dead code (the short-circuit in CheckStep).

2. **`PLANNER_MAX_COST` loaded but never enforced**: Greptile searched for all `CheckStep` call sites in the codebase, confirmed none pass `"planner"`, and concluded the env var is dead configuration. This is a grep-loop pattern: load a value → search for all consumers → confirm none exist.

**This grep-loop capability is significant.** It catches bugs that require understanding cross-file data flow, not just local code patterns. CodeRabbit's static analysis catches syntax and pattern issues, but Greptile catches *semantic* disconnects between components.

### CodeRabbit: Best at documentation and test safety

CodeRabbit excelled at:

- **Documentation drift**: Caught that the spec still said "Unknown models return $0" when the implementation changed to fallback pricing. Also caught the "Missing from Current Pipeline" list still mentioning cost tracking.
- **Markdown linting**: Flagged heading level increments (MD001).
- **Test safety**: Identified a potential index panic in `dossier_test.go` where `t.Errorf` (non-fatal) was used before indexing into a potentially empty slice.
- **API research**: Used web queries to verify that `genai.NewContentFromText` with a "user" role is incorrect for system instructions — a finding that required external API documentation.
- **Broken links**: Caught a relative link pointing to `docs/design.md` from within the `docs/` directory (should be `design.md`).

CodeRabbit's weakness: it duplicated Greptile's findings (architect budget check, spec env vars) but with less precise reasoning.

### Codex Connector: Best at runtime behavior analysis

Codex found two issues the others missed:

1. **Budget check placement**: Identified that `CheckStep("implementer")` runs after `CreatePatchPlan` but the expensive `GenerateFileContent` calls happen later without a re-check. This is a runtime ordering bug that requires understanding the execution flow, not just static code patterns.

2. **Flag ignored in CLI**: `RunAgent` returns `BudgetExceeded` but `cmd/implementer/main.go` never checks it, proceeding to create a PR from partial output. This is a "value computed but never consumed" pattern.

Codex's weakness: fewer findings overall, and no documentation or test safety analysis.

## Unique Finds per Reviewer

| Finding | Greptile | CodeRabbit | Codex |
|---------|----------|------------|-------|
| Architect budget dead code | First to find | Duplicate | -- |
| PLANNER_MAX_COST dead config | First to find | -- | -- |
| Malformed env var silent discard | First to find | -- | -- |
| Budget re-check after generation loop | -- | -- | First to find |
| BudgetExceeded flag ignored in CLI | -- | -- | First to find |
| dossier_test.go index panic | -- | First to find | -- |
| Spec doc heading levels | -- | First to find | -- |
| Spec doc unknown-model description drift | -- | First to find | -- |
| JOURNEY.md broken relative link | -- | First to find | -- |
| genai SystemInstruction API misuse | -- | First to find (web research) | -- |
| Mixed-model report cosmetic issue | First to find | -- | -- |

## Key Takeaway

**No single reviewer catches everything.** The combination of all three produced significantly better coverage than any one alone:

- **Greptile** for cross-file semantic analysis (grep loops, data flow tracing)
- **CodeRabbit** for documentation consistency, test safety, static analysis, and API correctness (via web research)
- **Codex** for runtime behavior and control flow ordering bugs

For this project's use case (automated PR review before human review), running all three in parallel is the best strategy. Total review time was under 2 minutes for all three combined.

## Greptile's Grep-Loop Capability

Worth highlighting as a differentiator: Greptile appears to use an iterative search strategy where it:

1. Identifies a function call or value
2. Greps the codebase for where that value is produced/consumed
3. Follows the chain across files
4. Identifies breaks in the chain (dead code, unused config, missing consumers)

This is the same pattern a human reviewer would use (`grep -rn "CheckStep" | grep "architect"` → `grep -rn "StepCaps\[\"architect\"\]"` → confirm nothing populates it). CodeRabbit's analysis appears to be more AST/pattern-based and doesn't perform this kind of iterative cross-file tracing with the same depth. Codex does some control-flow analysis but focuses on runtime ordering rather than data-flow completeness.
