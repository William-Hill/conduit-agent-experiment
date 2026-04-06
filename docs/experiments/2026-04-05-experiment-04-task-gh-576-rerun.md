# Experiment 04: HTTP Status Codes Re-Run (With Pipeline Fixes)

- **Date:** 2026-04-05
- **Task ID:** task-gh-576 (same as experiment 02)
- **Source:** GitHub issue ConduitIO/conduit#576
- **Difficulty:** L1
- **Blast Radius:** low
- **Run ID:** run-task-gh-576-20260405-212335
- **Duration:** 142 seconds
- **LLM Calls:** 8 (1 archivist, 3 implementer [1 plan + 2 file generations], 1 architect [round 1], 2 implementer-revise, 1 architect [round 2])
- **Final Status:** failed
- **Architect Recommendation:** revise (high confidence)
- **Revisions:** 1

## Introduction

This experiment re-runs the same task as experiment 02 (ConduitIO/conduit#576: HTTP status codes for validation errors) with five pipeline fixes applied:

1. **New-file artifact capture** — newly created files are now stored in run artifacts and passed to the architect
2. **Baseline verifier state** — verifier commands run before the patch to classify environmental vs patch-caused failures
3. **Expanded verifier allowlist** — shellcheck, yamllint, actionlint, test, cat now permitted
4. **Cross-file naming consistency** — generated sibling file contents are passed to each per-file LLM call
5. **Architect-to-implementer revision loop** — on "revise", the implementer re-generates files with architect feedback (max 1 revision round)

The purpose is to measure the concrete impact of these fixes on the failure mode observed in experiment 02 (cross-file naming inconsistency) and to observe whether new failure modes emerge.

## Hypothesis

**Primary:** H2 — AI agents perform best on low-risk, narrow-blast-radius tasks.

**Secondary:** H3 — A dedicated architectural review role improves quality over a single coding-agent workflow.

**Tertiary (new for this run):** The five pipeline fixes should eliminate the cross-file naming inconsistency observed in experiment 02 and the environmental confounder observed in experiments 01 and 03. If a different failure mode appears, it reveals a deeper layer of the problem.

## Prediction (retrofitted)

The sibling contents fix (fix 4) should prevent the naming divergence that caused experiment 02's build failure. The baseline verifier (fix 2) should correctly classify the pre-existing `go vet` unreachable-code issue as environmental. The revision loop (fix 5) should fire if the architect recommends "revise", giving the implementer a second chance.

The prediction is that the cross-file naming inconsistency will be eliminated, but whether the overall run succeeds depends on whether the LLM's semantic understanding of Conduit's error-handling patterns is sufficient to produce a correct fix.

## Method

### Task definition

Same as experiment 02 — `data/tasks/task-gh-576.json` with `verifier_commands: ["go build ./...", "go vet ./...", "go test ./pkg/http/api/..."]`.

### Configuration

- `allow_push: false` (dry run)
- `use_worktree: true`
- `max_difficulty: L2`, `max_blast_radius: medium`, `max_files_changed: 10`
- **`max_revisions: 1`** (new — enables one revision round)
- All agent roles: `gemini-2.5-flash`

### Pipeline fixes active

All five fixes from the `feature/experiment-failure-fixes` branch are active. This is the first run exercising the complete fix set.

### Conduit checkout

ConduitIO/conduit @ `cf8b7ed` (same as experiments 01-03)

### Invocation

```bash
CONDUIT_REPO_PATH=/Users/william-meroxa/Development/conduit \
  go run ./cmd/experiment run \
  --task data/tasks/task-gh-576.json \
  --models configs/models.yaml
```

## Results

### Triage

**Decision:** accept. **Reason:** "task within policy limits and dossier has relevant context."

### Archivist

Succeeded on first LLM call (no JSON parse failure, no fallback).

### Implementer

The plan targeted **2 files** (down from 10 in experiment 02):

1. `pkg/http/api/status/status.go` — extended error mapping
2. `pkg/http/api/status/status_test.go` — test coverage

**Patch plan summary:**

> "The plan addresses the task of returning proper HTTP status codes for validation errors by enhancing the error mapping logic in `pkg/http/api/status/status.go`. Specifically, it will extend `PipelineError` and `ConnectorError` functions to explicitly map a broader range of pipeline and connector-specific validation errors (e.g., missing names, invalid characters, length limits) to `codes.InvalidArgument` (HTTP 400). Additionally, the general `codeFromError` function will be updated to map common configuration validation errors (e.g., mandatory fields, invalid field values) and plugin validation errors to `codes.InvalidArgument`. Existing API handlers in `pipeline_v1.go` and `connector_v1.go` already use these `status` helper functions, so no direct changes are needed in the handlers themselves, ensuring consistent error responses for invalid requests."

This plan is dramatically more focused than experiment 02's 10-file plan. It correctly identifies that the handlers already use the status helper functions, so only the status mapper needs changing.

### Baseline Verifier

| Command | Baseline Exit Code | Classification |
|---------|-------------------|----------------|
| `go build ./...` | 0 | (clean baseline) |
| `go vet ./...` | 1 | **environment** |
| `go test ./pkg/http/api/...` | 0 | (clean baseline) |

The baseline correctly identified `go vet ./...` as a pre-existing failure. This is the same `pkg/lifecycle-poc/funnel/worker.go:429: unreachable code` issue observed in experiments 01 and 03.

### Verifier (Round 1 — after initial generation)

| Command | Exit Code | Classification |
|---------|-----------|----------------|
| `go build ./...` | 1 | **patch-caused** |
| `go vet ./...` | 1 | **environment** |
| `go test ./pkg/http/api/...` | 1 | **patch-caused** |

Build error:

```
pkg/http/api/status/status.go:18:2: no required module provides package
  github.com/conduitio/conduit/pkg/config; to add it:
  go get github.com/conduitio/conduit/pkg/config
```

The agent imported `github.com/conduitio/conduit/pkg/config` to reference `config.ErrDuplicateID`, but **this package does not exist in Conduit**. This is a hallucinated import — the agent assumed a plausible package structure that doesn't match reality.

### Architect (Round 1)

The architect recommended **revise** — and the revision loop fired.

### Implementer Revision (Round 1)

The pipeline re-invoked `ReviseFileContent` for both files, passing the architect's feedback. Two revision LLM calls were made (one per file).

### Verifier (Round 2 — after revision)

All three commands still failed with the same build error. The revision did not fix the hallucinated import because the LLM lacks information about which packages exist in the target repository.

### Architect (Round 2)

**Recommendation:** revise (high confidence)

> "While the architectural approach and design choices for improving HTTP status code mapping are sound and align with best practices, the critical failure of `go build` and `go test` in the verification report means the patch is not shippable. A patch that introduces build errors or test regressions cannot be approved, regardless of the quality of its design. The implementer must resolve these fundamental issues before the patch can be re-evaluated for approval."

The revision loop exhausted its budget (`max_revisions: 1`) and the run was marked failed.

### Full Agent Sequence

```
triage → archivist → implementer → verifier → architect [revise]
  → implementer-revise → verifier → architect [revise, exhausted]
  → run marked failed
```

This is the first run to exercise the full revision loop. The 8-stage sequence (vs experiment 02's 5-stage sequence) demonstrates the loop working as designed, even though the revision didn't resolve the underlying issue.

## Analysis

### Comparison with Experiment 02

| Metric | Experiment 02 (no fixes) | Experiment 04 (all fixes) | Change |
|--------|--------------------------|---------------------------|--------|
| Files changed | 10 | 2 | **80% reduction** |
| LLM calls | 13 | 8 | **38% reduction** |
| Diff lines | 273 | 233 | Smaller |
| Cross-file naming | **Broken** | **Fixed** | Eliminated |
| Revision rounds | 0 | 1 | Loop functional |
| Baseline captured | No | Yes | Environmental classified |
| Build failure cause | Undefined symbols (naming) | Hallucinated import | **Different class** |

The most significant outcome: **the cross-file naming inconsistency from experiment 02 is gone**. The sibling contents fix worked exactly as designed. The agent no longer defines symbols in one file with names that diverge from references in another file.

### Failure classification

**Primary:** `insufficient_repository_context` — the agent imported `github.com/conduitio/conduit/pkg/config`, a package that does not exist in the Conduit module. The agent's model of Conduit's package structure is incomplete, and neither the dossier nor the sibling contents provide a full package inventory. The agent hallucinated a plausible import path based on naming conventions (`pkg/config` is a common Go package name) rather than checking the actual module structure.

This is a qualitatively different failure from experiment 02's `cross_file_inconsistency`. That failure was a mechanical coordination problem solvable by sharing state between generation calls. This failure is a **knowledge problem**: the agent doesn't know what packages exist in the target repository, and no amount of cross-file coordination can fix that.

### Why the revision loop didn't help

The revision loop re-generated both files with the architect's feedback ("the patch does not build"). However, the feedback didn't identify *which* import was wrong or what the correct package structure is. The implementer-revise call had the same context gap as the original call: no information about which packages exist. The revision produced the same hallucinated import because the underlying context deficit was unchanged.

This reveals an important design insight: **the revision loop is effective for mechanical issues (naming, syntax, missing cases) but not for knowledge gaps**. When the agent lacks information that isn't in its context, retrying with "please fix it" doesn't help because the agent doesn't know *what* to fix.

### What the agent did well

1. **Dramatically more focused plan.** Two files instead of ten. The agent correctly identified that only the status mapping module needs changing — the handlers already delegate to it. This is a better architectural understanding than experiment 02.

2. **Clean switch consolidation.** The `codeFromError` rewrite groups cases logically with comments: "Generic validation errors", "Unimplemented errors", "Precondition failed errors", "Already exists errors". This is cleaner than experiment 02's version.

3. **Moved `ValidationError` to `InvalidArgument`.** The original code mapped `conn_plugin.ValidationError` to `FailedPrecondition`. The agent correctly moved it to `InvalidArgument`, which aligns with the HTTP semantics described in the issue.

### What this reveals about the next layer of improvement

The progression across experiments 02 and 04 tells a clear story:

- **Experiment 02:** Mechanical coordination failure (naming inconsistency between files). Fixed by sharing state.
- **Experiment 04:** Knowledge failure (hallucinated package import). Cannot be fixed by sharing state or retrying — requires providing the agent with accurate information about the target repository's structure.

The fix for this class of failure is **richer repository context in the dossier**: a package inventory, an import graph, or a symbol table that the agent can reference when deciding which packages to import. The symbol extractor (`internal/ingest/symbol_extractor.go`) already extracts Go AST information; the gap is that this information is not included in the implementer's generation prompt.

## Verdict

**H2 (agents perform best on low-risk tasks): Still partially refuted, but progress is measurable.** The plan quality improved dramatically (2 files vs 10), and the mechanical coordination failure is eliminated. The remaining failure is a knowledge gap, not a capability gap. Given accurate repository context, this task class may become feasible.

**H3 (architectural review adds value): Supported, with nuance.** The architect correctly identified the build failure and the sound architectural approach. However, the architect's feedback was too generic ("resolve these fundamental issues") to guide the revision loop effectively. More specific feedback ("the import `pkg/config` does not exist; consider using existing error sentinels in `pkg/foundation/cerrors`") would have been more actionable. This suggests the architect's value is highest for *detecting* problems and lowest for *prescribing* fixes.

**Pipeline fixes assessment:** Four of five fixes demonstrated measurable impact:

| Fix | Impact in this run |
|-----|-------------------|
| New-file capture | No new files in this task — not exercised |
| Baseline verifier | **Working** — `go vet` correctly classified as environmental |
| Non-Go allowlist | Not exercised (Go-only task) |
| Cross-file consistency | **Working** — naming inconsistency eliminated |
| Revision loop | **Working** — fired and completed one round, but couldn't fix a knowledge gap |

## Limitations

- **Same task, same model, same checkout.** Comparing experiments 02 and 04 is not a controlled experiment; both the pipeline fixes AND LLM non-determinism contribute to differences. The more focused plan (2 vs 10 files) may be a non-deterministic outcome rather than a fix-caused improvement.
- **Revision loop tested once.** A single revision round on a single task does not validate the loop broadly. The loop's inability to fix this particular failure is informative but does not prove it can't fix other classes of architect feedback.
- **LLM non-determinism.** The hallucinated import (`pkg/config`) may not reproduce on every run. A different run might hallucinate a different package or might coincidentally pick an existing one.
- **Retrofitted prediction.** The prediction that cross-file naming would be fixed was written with strong prior knowledge that the fix targets exactly this failure mode.

## References

- Run artifacts: `data/runs/run-task-gh-576-20260405-212335/` (run.json, dossier.json, evaluation.json, report.md)
- Comparison run: `data/runs/run-task-gh-576-20260405-151540/` (experiment 02)
- Task definition: `data/tasks/task-gh-576.json`
- Original issue: ConduitIO/conduit#576
- Build error: `pkg/http/api/status/status.go:18` — hallucinated import `github.com/conduitio/conduit/pkg/config`
- Pipeline fixes: `feature/experiment-failure-fixes` branch, commits `bda1e02` through `21db1f6`
- PRD hypotheses: `docs/design.md` section 7.9
- PRD failure taxonomy: `docs/design.md` section 7.18
- Symbol extractor (potential fix): `internal/ingest/symbol_extractor.go`
