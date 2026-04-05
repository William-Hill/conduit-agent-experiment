# Experiment 01: Fix Docs Drift in Pipeline Config Example

- **Date:** 2026-04-05
- **Task ID:** task-001
- **Source:** seeded
- **Difficulty:** L1
- **Blast Radius:** low
- **Run ID:** run-task-001-20260405-144421
- **Duration:** 116 seconds
- **LLM Calls:** 4
- **Final Status:** failed
- **Architect Recommendation:** revise (medium confidence)

## Introduction

This is the first end-to-end run of the conduit-agent-experiment tool against a real Conduit repository checkout. The task was intentionally chosen as a smoke test: a seeded L1 docs-drift task with no code changes expected, designed to validate that the full pipeline (triage, archivist, implementer, verifier, architect) executes to completion and produces coherent artifacts.

The run illuminates H2 (agents perform best on low-risk, narrow-blast-radius tasks) and H3 (a dedicated architectural review role improves quality). It also provides an unplanned observation about the verifier's susceptibility to environmental confounders.

## Hypothesis

**Primary:** H2 — AI agents perform best on low-risk, narrow-blast-radius tasks.

**Secondary:** H3 — A dedicated architectural review role improves quality over a single coding-agent workflow.

## Prediction (retrofitted)

H2 predicts that an L1/low docs-drift task is within the agent's sweet spot. The agent should identify the correct files, propose a relevant update, and produce a reviewable patch. Verification should pass (it's a docs change with no code impact). The architect should approve or suggest minor revisions.

H3 predicts that even on a trivial task, the architect will surface something a single coding agent would miss: a mismatch between the proposed change and existing ADR guidance, or a risk the implementer didn't mention.

## Method

### Task definition

```json
{
  "id": "task-001",
  "title": "Fix docs drift in pipeline config example",
  "source": "seeded",
  "description": "Review pipeline configuration examples in docs and update any that no longer match current config behavior.",
  "labels": ["docs", "drift"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "Docs updated to match current config behavior",
    "No code changes required",
    "Links and formatting validated"
  ],
  "verifier_commands": ["go build ./...", "go vet ./..."],
  "status": "pending"
}
```

Note: `verifier_commands` was added after the first run attempt (which used auto-detected `make test`) to isolate verifier signal from known environmental issues. The run documented here used the auto-detected commands before this override was added. See Limitations for the impact.

### Configuration

- `allow_push: false` (dry run)
- `use_worktree: true`
- `max_difficulty: L2`, `max_blast_radius: medium`, `max_files_changed: 10`
- All agent roles: `gemini-2.5-flash`

### Verifier commands (as executed)

This run used auto-detected commands from the dossier builder (the `verifier_commands` override was added after this run):
1. `make test`
2. `go build ./...`

### Conduit checkout

ConduitIO/conduit @ `cf8b7ed` (go.mod: bump google.golang.org/grpc from 1.79.3 to 1.80.0)

### Invocation

```bash
CONDUIT_REPO_PATH=/Users/william-meroxa/Development/conduit \
  go run ./cmd/experiment run \
  --task data/tasks/task-001.json \
  --models configs/models.yaml
```

## Results

### Triage

**Decision:** accept. **Reason:** "task within policy limits and dossier has relevant context."

### Archivist

The archivist LLM call returned invalid JSON (parse error: `invalid character 'S' after top-level value`). The system fell back to the keyword-based dossier builder. This means the dossier was assembled from keyword matching against the file inventory rather than LLM-enhanced retrieval. The resulting dossier identified relevant files and docs but produced generic verifier commands (`make test`, `go build ./...`) rather than task-appropriate ones.

### Implementer

The implementer produced a **42-line diff** modifying one file: `examples/processors/pipeline-extract-field-transform.yml`.

Changes made:
- Bumped pipeline version from `2.0` to `2.1`
- Replaced deprecated `format.type: "structured"` and `format.options: "id:int,name:string,company:string,trial:bool"` with the modern YAML block format:
  ```yaml
  format.structured.schema: |
    - name: id
      type: int
    - name: name
      type: string
    ...
  ```
- Replaced deprecated processor reference `type: extractfieldpayload` with `plugin: builtin:field.extract`
- Updated the pipeline description to reflect the changes

This is a plausible, targeted modernization of an example file. The implementer correctly identified that this specific example used deprecated syntax while other pipeline examples appeared up to date.

### Verifier

| Command | Exit Code | Result |
|---------|-----------|--------|
| `make test` | 1 | FAIL |
| `go build ./...` | 0 | PASS |

The `make test` failure was caused by **two pre-existing issues on the development machine**, not by the agent's patch:

1. `TestVerifyUnified` in `pkg/plugin/processor/builtin/internal/diff/difftest` failed with `diff tool not available: exit status 2`. This test fails on pristine Conduit main with identical output (verified independently).
2. `TestCtxLoggerWithoutHooks` and `TestCtxLoggerWithHooks` in `pkg/foundation/log` failed under the full test suite but pass in isolation. Likely a parallelism-sensitive flake.

The agent's change was to a YAML example file with no Go code impact. `go build ./...` passed cleanly. The `make test` failure is an environmental confounder, not a patch failure.

### Architect

**Recommendation:** revise (medium confidence)

Full review:

> "The patch cannot be approved in its current state primarily because the verification report indicates a critical `make test` failure. This immediately undermines confidence in the proposed changes, suggesting the new configuration syntax or processor usage may be incorrect or break existing functionality. Furthermore, there is an architectural misalignment regarding the choice of the replacement processor (`field.extract` vs. `field.set`) when cross-referenced with ADR `20231117-better-processors.md`. The change to `builtin:generator`'s format also appears to be a non-backward-compatible syntax update, which contradicts the implementer's assumption. These issues require investigation, clarification, and resolution before the patch can be confidently merged."

The architect made three distinct points:

1. **Test failure concern:** The architect correctly noted the failing tests but attributed them to the patch rather than the environment. This is a reasonable conclusion given the information available (the architect does not have access to baseline test results).
2. **ADR cross-reference:** The architect cited ADR `20231117-better-processors.md` and questioned whether `field.extract` is the correct replacement for `extractfieldpayload` versus `field.set`. This demonstrates active cross-referencing of supplemental documents against the proposed change.
3. **Backward compatibility flag:** The architect flagged the `builtin:generator` format change as potentially non-backward-compatible, contradicting the implementer's implicit assumption.

Points 2 and 3 are architecturally substantive critiques that would not have surfaced from the implementer alone.

## Analysis

### Failure classification

**Primary:** `environment_setup_failure` — the verifier's `make test` failure was caused by the development machine's missing `diff` tool and a parallelism-sensitive test flake, not by the agent's patch. This confounder propagated into the architect's reasoning, causing it to blame the patch for a failure the patch did not cause.

**Secondary observation:** The archivist's JSON parse failure is a separate reliability issue. When the archivist falls back to keyword-based dossier building, the verifier command selection degrades to generic defaults (`make test`) instead of task-appropriate commands. This created a chain: archivist failure -> generic commands -> environmental false negative -> architect misattribution.

### What the agent did well

The implementer identified a real docs-drift candidate and produced a targeted, plausible modernization. The file selection was correct. The changes were proportionate to the task. The patch format was clean (one file, 42 lines, minimal blast radius).

The architect demonstrated cross-referencing ability by citing a specific ADR (`20231117-better-processors.md`) and questioning the semantic correctness of the processor name substitution. This is exactly the kind of review that PRD section 7.14 Requirement 6 calls for.

### What went wrong and why

The verifier signal was corrupted by an environmental confounder. Because the architect treats verifier output as ground truth ("the verification report indicates a critical make test failure"), any environmental noise in the verifier directly reduces architect reasoning quality. The pipeline currently has no mechanism to distinguish "the patch broke the build" from "the build was already broken."

## Verdict

**H2 (agents perform best on low-risk tasks): Inconclusive.** The agent produced a plausible L1 patch, but the corrupted verifier signal prevents us from confirming that the patch would have been approved under clean conditions. The implementer's output is encouraging (correct file, correct changes, proportionate scope), but we cannot claim success because the full pipeline's terminal state is `failed`.

**H3 (architectural review adds value): Partially supported.** Even under a corrupted verifier signal, the architect surfaced two substantive architectural critiques (ADR cross-reference, backward compatibility concern) that the implementer alone would not have produced. However, the architect also misattributed the environmental test failure to the patch, demonstrating that the architect role is only as strong as its inputs. H3 is supported in the "value-add" sense but with the important caveat that architect quality degrades when verifier signal is noisy.

## Limitations

- **n=1.** A single docs-drift run cannot validate H2 broadly. The task was the simplest possible smoke test; performance on L1 docs tasks does not predict performance on L1 code tasks or L2 tasks.
- **Corrupted verifier signal.** The environmental `make test` failure means we never observed the pipeline's behavior under clean verification. A re-run with the narrower `verifier_commands` override would produce a more informative result, but was not performed because we prioritized moving to a real-code task (experiment 02).
- **Archivist fallback.** Because the archivist fell back to keyword mode, we did not observe the LLM-enhanced dossier's impact on verifier command selection or implementer context quality. The run reflects a degraded dossier path, not the intended LLM-enhanced path.
- **Retrofitted prediction.** The prediction was written after observing results. The fact that H2's prediction partially matches the outcome should be weighed accordingly.

## References

- Run artifacts: `data/runs/run-task-001-20260405-144421/` (run.json, dossier.json, evaluation.json, report.md)
- Task definition: `data/tasks/task-001.json`
- Target file: `examples/processors/pipeline-extract-field-transform.yml` in ConduitIO/conduit
- PRD hypotheses: `docs/design.md` section 7.9
- PRD failure taxonomy: `docs/design.md` section 7.18
- ADR cited by architect: `20231117-better-processors.md` in ConduitIO/conduit
