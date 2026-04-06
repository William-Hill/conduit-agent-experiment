# Experiment Log: Agent-Assisted Maintenance on Conduit

This directory records structured observations from end-to-end runs of the conduit-agent-experiment tool against a fork of the [Conduit](https://github.com/ConduitIO/conduit) streaming platform. Each experiment is one complete pipeline execution: task intake, triage, dossier generation, patch implementation, verification, and architectural review.

The purpose of this log is to accumulate evidence for or against the hypotheses stated in the project's PRD (docs/design.md, section 7.9) and to produce concrete, citable material for the OSA Community talk.

## Hypotheses Under Test

These hypotheses are drawn verbatim from PRD section 7.9. Individual experiments cite them by number.

- **H1.** Repositories with ADRs, design docs, tests, CI, and contribution conventions are better candidates for agent-assisted maintenance.
- **H2.** AI agents perform best on low-risk, narrow-blast-radius tasks.
- **H3.** A dedicated architectural review role improves quality over a single coding-agent workflow.
- **H4.** Human approval remains essential for semantic, compatibility, and governance-sensitive changes.

## Methodology

### Target repository

All experiments target the same fork and upstream state unless otherwise noted:

- **Upstream:** ConduitIO/conduit @ `cf8b7ed` (`go.mod: bump google.golang.org/grpc from 1.79.3 to 1.80.0 (#2450)`)
- **Fork:** William-Hill/conduit (local checkout at `/Users/william-meroxa/Development/conduit`)

### Tool configuration

- **Pipeline mode:** dry run (`allow_push: false`, `allow_merge: false`)
- **Execution isolation:** git worktree per run (`use_worktree: true`)
- **LLM provider:** Gemini via OpenAI-compatible endpoint, `gemini-2.5-flash` for all agent roles
- **Policy caps:** max difficulty L2, max blast radius medium, max 10 files changed

### Verifier commands

Full `make test` is unreliable on this development machine due to two pre-existing issues unrelated to any agent patch: a missing `diff` CLI tool that causes `TestVerifyUnified` to fail in every run, and a parallelism-sensitive log test that fails under full test suite load but passes in isolation. To prevent environmental noise from corrupting verifier signal, tasks specify narrower `verifier_commands` scoped to the relevant subsystem. This override mechanism was added during the first run session (see Task struct's `VerifierCommands` field). Each experiment's Method section documents the exact commands used.

### A note on predictions

Each experiment includes a "Prediction" section stating what the PRD hypotheses would have predicted for that task, had we articulated predictions before running. **These predictions were not pre-registered.** The PRD hypotheses existed before any runs were executed, but per-run predictions were written after observing results. We are explicit about this so the reader can weigh the evidence accordingly. Where a prediction aligns suspiciously well with the outcome, the Limitations section flags this.

## Failure Taxonomy

When experiments fail, the Analysis section classifies the failure using the PRD's taxonomy (section 7.18, reformatted here as identifiers):

| Code | Description |
|------|-------------|
| `retrieval_failure` | Dossier missed critical files or context |
| `task_misclassification` | Task difficulty or blast radius was wrong |
| `implementation_hallucination` | Agent invented code that doesn't compile or exist |
| `semantically_incorrect_fix` | Code compiles but doesn't solve the problem |
| `test_false_confidence` | Tests pass but the change is wrong |
| `architecture_drift` | Patch contradicts ADRs or system boundaries |
| `environment_setup_failure` | Failure caused by dev environment, not the patch |
| `insufficient_repository_context` | Agent lacked enough information to act correctly |
| `excessive_iteration_cost` | Too many retries or LLM calls for the result |
| `human_rejection` | Human reviewer declined the patch |

Observations that don't fit the existing taxonomy are flagged explicitly as candidate extensions rather than force-fitted into an existing category.

## Experiment Index

| # | Date | Task | Difficulty | Blast Radius | Final Status | Duration | Key Finding |
|---|------|------|------------|-------------|-------------|----------|-------------|
| 01 | 2026-04-05 | task-001: Docs drift in pipeline config example | L1 | low | failed | 116s | Tool produced credible patch; failure was environmental, not agent-caused. Architect provided useful review even under corrupted verifier signal. |
| 02 | 2026-04-05 | task-gh-576: HTTP status codes for validation errors | L1 | low | failed | 182s | Agent's architectural intent was correct; build failed due to cross-file naming inconsistency in generated code. Architect caught both the build failure and a deeper semantic flaw. |
| 03 | 2026-04-05 | task-gh-645: Automate version constant update in built-in connectors | L1 | low | failed | 95s | Most focused run (2 files, 5 LLM calls). Architect caught a force-push anti-pattern in the CI workflow. Verifier provided zero signal on the non-Go deliverable. Primary artifact (script) lost due to git diff limitation. |
| 04 | 2026-04-05 | task-gh-576: HTTP status codes (re-run with pipeline fixes) | L1 | low | failed | 142s | **Re-run of exp 02 with all 5 fixes.** Cross-file naming inconsistency eliminated. Revision loop fired (first time). Baseline verifier classified go vet as env. New failure: hallucinated import (`pkg/config` doesn't exist). Reveals knowledge gap as next improvement target. |

## How to Read an Entry

Each experiment follows an extended IMRAD structure: Introduction, Hypothesis, Prediction (retrofitted), Method, Results, Analysis, Verdict, Limitations, References. The Verdict section is the primary citation target for the talk. The Analysis section maps failures to the taxonomy above. The Limitations section states what a single run cannot prove.
