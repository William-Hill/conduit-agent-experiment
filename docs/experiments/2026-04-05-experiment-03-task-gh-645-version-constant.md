# Experiment 03: Automate Version Constant Update in Built-in Connectors

- **Date:** 2026-04-05
- **Task ID:** task-gh-645
- **Source:** GitHub issue ConduitIO/conduit#645
- **Difficulty:** L1
- **Blast Radius:** low
- **Run ID:** run-task-gh-645-20260405-193807
- **Duration:** 95 seconds
- **LLM Calls:** 5 (1 archivist, 3 implementer [1 plan + 2 file generations], 1 architect)
- **Final Status:** failed
- **Architect Recommendation:** revise (high confidence)

## Introduction

This is the third end-to-end run and the first against a CI/infrastructure task rather than a code-change task. The issue describes a maintenance problem: built-in connectors have manually maintained version constants that diverge from actual release tags. The fix is an automated mechanism (CI action) to ensure these constants stay in sync.

This task tests a different dimension of agent capability than experiments 01 and 02. Rather than modifying Go source code, the agent must produce a shell script and a GitHub Actions workflow change. The correctness criteria are different: there is no compiler to catch errors, and the verifier commands (`go build`, `go vet`) do not exercise the actual deliverable (a CI workflow).

The run primarily tests H2 (agents perform best on low-risk tasks) with a secondary lens on H3 (architectural review value) and H4 (human approval remains essential).

## Hypothesis

**Primary:** H2 — AI agents perform best on low-risk, narrow-blast-radius tasks.

**Secondary:** H3 — A dedicated architectural review role improves quality over a single coding-agent workflow. H4 — Human approval remains essential for semantic, compatibility, and governance-sensitive changes.

## Prediction (retrofitted)

H2 predicts this task should be within the agent's sweet spot: L1 difficulty, low blast radius, narrow scope (a script and a workflow file). However, CI/infrastructure tasks have a different correctness surface than code changes. The verifier commands (`go build`, `go vet`) cannot exercise a shell script or a GitHub Actions workflow, so verification provides no signal about the actual deliverable. This means quality depends entirely on the architect and human reviewer.

H3 predicts the architect will catch issues with the CI approach that the implementer would not self-identify, particularly around release workflow safety.

H4 predicts that even if the agent produces a plausible CI configuration, human review is essential to assess operational risk (release integrity, tag semantics, workflow interactions).

## Method

### Task definition

```json
{
  "id": "task-gh-645",
  "title": "Automate version constant update in built-in connectors",
  "source": "github#645",
  "description": "Built-in connectors each have a manually maintained version constant that diverges from actual release tags. The fix is a CI action that ensures the version constant matches the release tag at release time.",
  "labels": ["housekeeping"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "GitHub Actions workflow that checks or updates version constants at tag time",
    "Version constant format is documented",
    "Existing connector behavior is unchanged"
  ],
  "verifier_commands": ["go build ./...", "go vet ./..."],
  "issue_number": 645,
  "status": "pending"
}
```

### Configuration

- `allow_push: false` (dry run)
- `use_worktree: true`
- `max_difficulty: L2`, `max_blast_radius: medium`, `max_files_changed: 10`
- All agent roles: `gemini-2.5-flash`

### Verifier commands (as executed)

1. `go build ./...`
2. `go vet ./...`

Note: neither command exercises the actual deliverable (a shell script and a workflow YAML modification). This is a known limitation for CI/infrastructure tasks where the verifier's command allowlist does not include workflow validators or shell linters.

### Conduit checkout

ConduitIO/conduit @ `cf8b7ed` (go.mod: bump google.golang.org/grpc from 1.79.3 to 1.80.0)

### Invocation

```bash
CONDUIT_REPO_PATH=/Users/william-meroxa/Development/conduit \
  go run ./cmd/experiment run \
  --task data/tasks/task-gh-645.json \
  --models configs/models.yaml
```

## Results

### Triage

**Decision:** accept. **Reason:** "task within policy limits and dossier has relevant context."

### Archivist

The archivist succeeded on the first LLM call (no JSON parse failure, no retry, no keyword fallback). This is the first experiment in which the archivist produced a fully LLM-enhanced dossier without degradation.

### Implementer

The plan targeted **2 files** with 2 generation calls:

1. `.github/workflows/release.yml` — modified to add a step that runs an update script before GoReleaser
2. `scripts/update-connector-versions.sh` — a new shell script to find and update connector dependencies

**Patch plan summary (quoted from run artifacts):**

> "This plan automates the update of built-in connector versions in the `go.mod` file during the Conduit release process. A new shell script will be created to identify all `conduit-connector-*` dependencies and update them to their `@latest` versions using `go get`. This script will then execute `go mod tidy`. A new step will be added to the `.github/workflows/release.yml` workflow, specifically in the `release` job before `GoReleaser`, to run this script. The changes made to `go.mod` and `go.sum` will be committed by the CI job, ensuring the release tag points to a commit that includes the latest connector versions."

The plan is coherent and addresses the core issue: automating the version constant sync at release time.

**Changes in the diff (`.github/workflows/release.yml` only):**

The workflow step added:
- Runs `scripts/update-connector-versions.sh` to update connector dependencies
- Checks if `go.mod`/`go.sum` changed, commits if so
- **Force-pushes the release tag** to point to the new commit: `git tag -f "${GITHUB_REF_NAME}" HEAD && git push -f origin "${GITHUB_REF_NAME}"`
- Skips the step for nightly builds via conditional: `if: ${{ !contains(github.ref, '-nightly') }}`

**Important artifact limitation:** The persisted diff (`implementer_diff` in run.json) is captured via `git diff`, which only shows changes to *tracked* files. The new script `scripts/update-connector-versions.sh` is a newly created file in the worktree but is *untracked* by git, so it does not appear in the diff artifact. The file was generated (2 generation calls occurred), but its content is not preserved in run artifacts because the worktree is cleaned up after the run. This is a tool limitation: `git diff` should be supplemented with a mechanism to capture newly created files.

### Verifier

| Command | Exit Code | Result |
|---------|-----------|--------|
| `go build ./...` | 0 | PASS |
| `go vet ./...` | 1 | FAIL |

The `go vet` failure was caused by a **pre-existing issue on the Conduit codebase**, not by the agent's patch:

```
pkg/lifecycle-poc/funnel/worker.go:429:3: unreachable code
```

This `go vet` failure reproduces identically on pristine Conduit main (verified independently). The agent's changes were to a GitHub Actions workflow YAML and a shell script — neither of which can cause a `go vet` failure in Go source code.

This is the **same class of environmental confounder** observed in experiment 01 (where `make test` failures from `TestVerifyUnified` and `TestCtxLoggerWithHooks` corrupted verifier signal). The `go vet` unreachable-code finding in `lifecycle-poc` is a pre-existing issue that contaminates every run that uses `go vet ./...` as a verifier command.

Additionally, neither verifier command (`go build`, `go vet`) exercises the actual deliverable. A shell script and a GitHub Actions workflow change cannot be validated by Go toolchain commands. The verifier provided **zero signal** about the quality of the agent's work on this task.

### Architect

**Recommendation:** revise (high confidence)

Full review:

> "The proposed patch is fundamentally flawed due to a reported `go vet` failure, indicating that the system is in a broken state after the dependency updates. Furthermore, the approach of force-pushing release tags is a critical anti-pattern that undermines release integrity and Git history, posing significant risks to consumers and development workflows. The absence of the actual update script also prevents a complete and thorough review of the core logic. Substantial revisions are required to address these critical issues before the patch can be considered for approval."

The architect made three distinct points:

1. **go vet failure attribution (incorrect):** The architect attributed the `go vet` failure to the agent's dependency updates. This is the same misattribution pattern observed in experiment 01: environmental noise in the verifier is treated as evidence against the patch. The failure has nothing to do with the agent's changes.

2. **Force-push anti-pattern (correct and significant):** The architect identified the `git tag -f` + `git push -f origin` approach as "a critical anti-pattern that undermines release integrity." This is a **valid, substantive architectural critique**. Force-pushing release tags in a CI workflow is dangerous: consumers who fetched the original tag get a different commit than those who fetch after the push; automated systems that trigger on tag events may fire twice; and the audit trail of what the tag originally pointed to is lost. A better approach would be to update dependencies *before* tagging, or to use a pre-release workflow that ensures the tag is only created after dependencies are up to date.

3. **Missing script (correct):** The architect noted "the absence of the actual update script also prevents a complete and thorough review." The script was generated but is invisible in the diff artifact because `git diff` does not capture untracked files. However, from the architect's perspective (which reviews the diff), this is a legitimate concern: the workflow references a script at `scripts/update-connector-versions.sh` that does not appear in the reviewed changeset.

## Analysis

### Failure classification

**Primary:** `environment_setup_failure` — the `go vet` failure is pre-existing and unrelated to the patch. This is the same confounder class as experiment 01. The experiment tool needs a mechanism to establish baseline verifier state before the agent's patch is applied, so environmental failures can be filtered from patch-caused failures.

**Secondary observation:** The verifier commands provided **zero signal** about the actual deliverable. Neither `go build` nor `go vet` can validate shell scripts or GitHub Actions YAML. This reveals a coverage gap in the verifier for non-Go tasks. CI/infrastructure tasks may need a different verifier strategy: YAML schema validation for workflow files, `shellcheck` for scripts, or at minimum a file-existence check for referenced resources.

**Tool limitation (newly observed):** The `git diff` artifact capture mechanism does not preserve newly created files. The shell script was the primary deliverable of this task, and its content was lost when the worktree was cleaned up. This means the run artifacts are incomplete for tasks that create new files. Future runs should supplement `git diff` with `git status --short` and content capture for untracked files.

### What the agent did well

1. **Focused plan.** Two files, 5 LLM calls, 95 seconds. The most economical run of the three experiments. The plan correctly identified the two deliverables needed: a script and a workflow change.

2. **Correct structural approach.** The plan's architecture is sound: a reusable script that updates connector dependencies, invoked by a CI step at release time, with a conditional to skip nightlies. This matches the issue's requirement.

3. **Workflow YAML competence.** The generated workflow step has correct GitHub Actions syntax (`${{ github.ref_name }}`, conditional `if` expression, proper indentation within the job). The agent demonstrated comfort with YAML-based CI configuration, not just Go source code.

### What went wrong and why

1. **The force-push approach** is the most significant issue the architect identified. The agent chose to update dependencies *after* the tag is pushed, then rewrite the tag to point to the updated commit. This ordering creates a window where the tag points to different commits for different consumers. The correct approach would be to ensure dependencies are up to date *before* the release tag is created, or to use a two-phase release workflow.

2. **The script is unverifiable.** Because the script is a new untracked file, it was not captured by `git diff` and is not in the run artifacts. Additionally, neither verifier command could test it. This is a dual gap: the tool doesn't capture it, and the tool doesn't verify it.

3. **The architect's force-push critique raises H4.** The force-push approach is not an obviously wrong answer; it is a judgment call about release workflow safety that requires operational context about how consumers interact with tags. This is exactly the kind of decision where human approval is essential (H4).

## Verdict

**H2 (agents perform best on low-risk tasks): Partially supported with important qualifiers.** The agent produced a focused, structurally correct plan for a low-risk task. The workflow YAML is syntactically valid. The plan addresses the issue's requirements. However, the primary deliverable (the script) is invisible in artifacts, and the approach has a genuine architectural flaw (force-push tags). H2 holds in the sense that the agent *can* produce CI/infrastructure work at L1, but the verifier and artifact capture provide no safety net for non-Go tasks. The agent's output requires heavier human review to compensate.

**H3 (architectural review adds value): Strongly supported.** The architect identified the force-push anti-pattern, which is the single most important finding in this run. Without architectural review, this change could be merged and create real operational risk. The architect also correctly flagged the missing script from the reviewed changeset, even though this was caused by a tool artifact gap rather than an agent generation failure.

**H4 (human approval is essential): Supported.** The force-push question is a judgment call about release workflow safety, not a correctness question. The verifier cannot evaluate it. The architect flagged it but recommended `revise`, not `reject`. A human reviewer with operational context (how are tags consumed? do downstream systems re-fetch? is there a tag protection policy?) needs to make the final call. This is a textbook case for H4.

## Limitations

- **n=1 for CI/infrastructure tasks.** This is the only experiment targeting non-Go infrastructure. Performance on CI tasks cannot be generalized from a single run.
- **Missing artifact.** The primary deliverable (shell script) was not captured in run artifacts due to the `git diff` limitation. We cannot fully evaluate the script's quality. A re-run with artifact capture for new files would be needed.
- **Verifier blind spot.** Neither verifier command exercised the deliverable. The verifier pass/fail signal is entirely about the Go codebase, not the CI configuration. Any assessment of quality comes from the architect and human review, not from automated verification.
- **Environmental confounder (again).** The `go vet` failure corrupted the architect's reasoning by providing false evidence of breakage. The architect's first point (go vet failure) was based on this noise; only points 2 and 3 reflect genuine critique. This is the same pattern as experiment 01 and suggests the tool needs baseline verifier state capture as a standard feature.
- **Retrofitted prediction.** The prediction about verifier blind spots for CI tasks was written after observing the run, not before. A genuine pre-run prediction would likely have been simpler: "expect success on L1 task."

## References

- Run artifacts: `data/runs/run-task-gh-645-20260405-193807/` (run.json, dossier.json, evaluation.json, report.md)
- Task definition: `data/tasks/task-gh-645.json`
- Original issue: ConduitIO/conduit#645
- Modified file: `.github/workflows/release.yml` in ConduitIO/conduit
- Missing artifact: `scripts/update-connector-versions.sh` (generated but not captured in `git diff`)
- Pre-existing go vet issue: `pkg/lifecycle-poc/funnel/worker.go:429` in ConduitIO/conduit
- PRD hypotheses: `docs/design.md` section 7.9
- PRD failure taxonomy: `docs/design.md` section 7.18
