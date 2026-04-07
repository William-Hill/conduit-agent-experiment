# Experiment 02: Return Proper HTTP Status Codes for Validation Errors

- **Date:** 2026-04-05
- **Task ID:** task-gh-576
- **Source:** GitHub issue ConduitIO/conduit#576
- **Difficulty:** L1
- **Blast Radius:** low
- **Run ID:** run-task-gh-576-20260405-151540
- **Duration:** 182 seconds
- **LLM Calls:** 13 (1 archivist, 11 implementer [1 plan + 10 file generations], 1 architect)
- **Final Status:** failed
- **Architect Recommendation:** revise (high confidence)

## Introduction

This is the first run against real Conduit source code (as opposed to the docs-drift smoke test in experiment 01). The task is derived from a genuine open issue on the upstream Conduit repository: all API errors are returned as HTTP 500 regardless of cause, and validation errors (e.g., starting a pipeline with no connectors, creating a connector with invalid config) should return HTTP 400 instead.

The task is classified L1/low, but it requires modifying Go source code across multiple packages (`pkg/http/api/status/`, `pkg/http/api/`, `pkg/connector/`, `pkg/foundation/cerrors/`, `pkg/pipeline/`). This makes it a meaningful test of the agent's ability to coordinate changes across file boundaries.

The run primarily tests H2 (agents perform best on low-risk tasks) and H3 (architectural review adds value). It also provides an unplanned but significant observation about a failure mode not explicitly anticipated in the PRD's failure taxonomy.

## Hypothesis

**Primary:** H2 — AI agents perform best on low-risk, narrow-blast-radius tasks.

**Secondary:** H3 — A dedicated architectural review role improves quality over a single coding-agent workflow.

## Prediction (retrofitted)

H2 predicts that an L1/low task should be within the agent's capability. However, this task requires coordinated changes across 5+ packages, touching error definition files, status-mapping logic, API handlers, and tests. The prediction is nuanced: H2 would predict success on the task *difficulty*, but the *breadth* of files involved (9 files changed, near the policy cap of 10) creates risk of coordination failures. If the agent succeeds, it strongly supports H2. If it fails, the failure mode (mechanical vs. conceptual) matters: a conceptual failure would weaken H2, while a mechanical coordination failure would be informative about the agent architecture's limitations without necessarily weakening H2 itself.

H3 predicts the architect will catch issues the implementer missed, especially around how Conduit's error types interact with the status-mapping mechanism. The architect should flag any change that alters public API behavior.

## Method

### Task definition

```json
{
  "id": "task-gh-576",
  "title": "Return proper HTTP status codes instead of 500 for validation errors",
  "source": "github#576",
  "description": "All API errors are returned as HTTP 500 regardless of cause. Starting a pipeline with no connectors returns 500 instead of 400, and creating a connector with invalid config returns 500 instead of 400. Error messages are also not documented in the Swagger/OpenAPI spec.",
  "labels": ["bug"],
  "difficulty": "L1",
  "blast_radius": "low",
  "acceptance_criteria": [
    "Validation errors return HTTP 400 instead of 500",
    "At least the two documented examples return correct status codes",
    "No changes to successful request behavior"
  ],
  "verifier_commands": [
    "go build ./...",
    "go vet ./...",
    "go test ./pkg/http/api/..."
  ],
  "issue_number": 576,
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
3. `go test ./pkg/http/api/...`

These commands were scoped to isolate the subsystem under change. `go test ./pkg/http/api/...` passes cleanly on pristine Conduit main (verified before run).

### Conduit checkout

ConduitIO/conduit @ `cf8b7ed` (go.mod: bump google.golang.org/grpc from 1.79.3 to 1.80.0)

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

The archivist LLM call succeeded this run (no JSON parse failure, no fallback to keyword mode). The enhanced dossier identified relevant files across `pkg/http/api/`, `pkg/connector/`, and `pkg/foundation/cerrors/`.

### Implementer

The implementer produced a **10-file patch** plan and then generated content for each file in separate LLM calls (1 plan call + 10 generation calls = 11 implementer LLM calls total). The resulting `git diff` shows 9 files with visible changes; one planned file's generated content produced no diff-visible change.

**Patch plan summary (quoted from run artifacts):**

> "This patch aims to correct API error responses by returning appropriate HTTP 400 status codes for validation errors instead of generic 500s. This will be achieved by enhancing the `pkg/http/api/status/status.go` module to map common validation-related error types (e.g., mandatory fields missing, invalid field values, ID/name length limits, duplicate IDs) to `codes.InvalidArgument`. Additionally, a specific endpoint (`UpdateConnector`) that directly returned a generic error without proper status wrapping will be fixed to use the correct status helper."

**Files changed:**

| File | Nature of change |
|------|-----------------|
| `pkg/foundation/cerrors/errors.go` | Added 4 new error constants: `ErrRequiredField`, `ErrInvalidValue`, `ErrInvalidLength`, `ErrAlreadyExists` |
| `pkg/connector/errors.go` | Added `ErrDuplicateID` constant |
| `pkg/http/api/status/status.go` | Rewrote `codeFromError()` switch: grouped FailedPrecondition cases, added InvalidArgument cases referencing `cerrors.ErrInvalidID`, `cerrors.ErrInvalidName`, `cerrors.ErrNameLengthExceeded` |
| `pkg/http/api/connector_v1.go` | Wrapped `cerrors.ErrEmptyID` return in `status.ConnectorError()` |
| `pkg/http/api/connector_v1_test.go` | Added `TestConnectorAPIv1_UpdateConnector_ValidationError` |
| `pkg/http/api/pipeline_v1.go` | No functional change (trailing newline) |
| `pkg/http/api/pipeline_v1_test.go` | Added `TestPipelineAPIv1_CreatePipeline_ValidationError` |
| `pkg/pipeline/errors.go` | No functional change (trailing newline) |
| `pkg/provisioning/config/errors.go` | No functional change (trailing newline) |

The patch is architecturally coherent in intent: define validation error types in the error packages, map them to gRPC `InvalidArgument` in the status mapper, fix a handler that bypassed the mapper, and add test coverage. This is the correct high-level approach to the issue.

### Verifier

| Command | Exit Code | Result |
|---------|-----------|--------|
| `go build ./...` | 1 | FAIL |
| `go vet ./...` | 1 | FAIL |
| `go test ./pkg/http/api/...` | 1 | FAIL |

All three commands failed with the same root cause:

```
pkg/http/api/status/status.go:82:27: undefined: cerrors.ErrInvalidID
pkg/http/api/status/status.go:83:27: undefined: cerrors.ErrInvalidName
pkg/http/api/status/status.go:84:27: undefined: cerrors.ErrNameLengthExceeded
```

The code does not compile. This is a genuine agent failure, not an environmental issue.

### Architect

**Recommendation:** revise

Full review:

> "The patch proposes a valuable improvement by standardizing API error responses for validation failures to `HTTP 400` (mapped from `codes.InvalidArgument`), which is good architectural practice. However, the verification report indicates a critical failure as the patch does not build or pass tests, preventing any further assessment of its correctness. Furthermore, a fundamental flaw exists in the error mapping mechanism: specific errors are not wrapping the generic `cerrors` types, which invalidates the assumption that `cerrors.Is` will correctly identify them for status code mapping. This will result in many validation errors still being reported as generic internal server errors (`HTTP 500`). Therefore, the patch requires significant revision to correct these fundamental issues before it can be considered for approval."

The architect made three distinct points:

1. **Acknowledged the intent as correct:** "a valuable improvement... good architectural practice." The architect correctly identified that the high-level approach is right even though the implementation is broken.
2. **Caught the build failure:** "the verification report indicates a critical failure as the patch does not build or pass tests." Straightforward and correct.
3. **Identified a deeper semantic flaw:** "a fundamental flaw exists in the error mapping mechanism: specific errors are not wrapping the generic `cerrors` types, which invalidates the assumption that `cerrors.Is` will correctly identify them." This goes beyond the build error. Even if the naming were fixed, the mapping in `codeFromError()` uses `cerrors.Is()` to check error identity, but the actual validation errors thrown by Conduit's orchestrator and handler layers may not wrap these specific sentinel values. The mapper would silently fall through to the `default: codes.Internal` case, returning 500 for the exact errors it was supposed to reclassify.

Point 3 is the most significant finding. The architect identified a correctness flaw that a build-fix-only approach would miss. A human reviewer who only fixed the undefined references would ship a change that compiles and tests green but doesn't solve the issue described in the original bug report.

## Analysis

### Failure classification

**Primary:** The build failure's root cause is a **cross-file naming inconsistency** in the generated code. The implementer defined four new error constants in `pkg/foundation/cerrors/errors.go`:

```go
ErrRequiredField = New("required field missing")
ErrInvalidValue  = New("invalid field value")
ErrInvalidLength = New("invalid length")
ErrAlreadyExists = New("already exists")
```

But in `pkg/http/api/status/status.go`, the switch statement references three *different* names that were never defined:

```go
cerrors.Is(err, cerrors.ErrInvalidID),
cerrors.Is(err, cerrors.ErrInvalidName),
cerrors.Is(err, cerrors.ErrNameLengthExceeded),
```

This happened because the implementer generates each file's content in a separate LLM call (see `internal/orchestrator/workflow.go:195,220` — `agents.GenerateFileContent()` is called once per file). Each call receives the patch plan and the target file path, but **does not receive the actual content generated for other files in the same plan**. The `cerrors/errors.go` generation call and the `status/status.go` generation call each independently chose names for the new error constants, and the names diverged.

This failure mode does not cleanly fit any single category in the PRD's failure taxonomy (section 7.18). It shares characteristics with `implementation_hallucination` (the agent referenced symbols that don't exist) and `insufficient_repository_context` (the agent lacked cross-file visibility during generation). However, the root cause is specifically **the per-file generation architecture's lack of shared state between calls**, which is a structural limitation of the tool, not a retrieval or hallucination problem in the usual sense.

**Observation:** This failure mode — where independently generated files reference incompatible symbols, types, or interfaces — may warrant its own taxonomy category. We flag it here as `cross_file_inconsistency` without formally extending the taxonomy, as a single observation is not sufficient to justify a taxonomy revision.

**Secondary:** The architect's deeper critique (point 3 above) identifies what would be a `semantically_incorrect_fix` if the patch were buildable. Even with the naming fixed, the error mapping mechanism is architecturally incomplete: validation errors in Conduit are not consistently wrapped with the sentinel values the mapper checks for. This secondary finding is not a failure of the tool (the tool correctly surfaced it via the architect role) but a finding about the complexity of the underlying issue.

### What the agent did well

1. **Correct architectural intent.** The plan correctly identified `pkg/http/api/status/status.go`'s `codeFromError()` as the central mapping point and proposed the right structural approach: define error sentinels, map them to gRPC codes, fix handlers that bypass the mapper, add tests.

2. **Test generation.** The agent wrote two new test functions (`TestConnectorAPIv1_UpdateConnector_ValidationError`, `TestPipelineAPIv1_CreatePipeline_ValidationError`) with appropriate gRPC status assertions. The test structure is correct even though the tests can't compile due to the upstream build failure.

3. **Handler fix.** The agent identified that `UpdateConnector` returned `cerrors.ErrEmptyID` directly without wrapping it in `status.ConnectorError()`, and fixed it. This is a legitimate bug fix that would be independently reviewable.

4. **Switch consolidation.** The agent grouped the `FailedPrecondition` cases in `codeFromError()` into a single multi-condition case, reducing duplication. This is a sensible refactor.

### What went wrong and why

The root cause is the per-file generation architecture. Each `GenerateFileContent()` call operates with:
- The patch plan (text summary of what to do)
- The target file path
- The current content of that specific file

It does *not* have:
- The actual generated content of other files in the same plan
- A shared symbol table or type registry
- The ability to verify that names it references in other packages were actually defined

This means the agent operates under a "consistency by convention" assumption: if the plan says "add error constants to cerrors," each file generation call independently guesses what those constants will be named. When the guesses diverge, the build breaks.

### Scope observation

The agent's plan touched 9 files (near the `max_files_changed: 10` policy limit). For a task classified L1/low, this breadth is notable. The original issue (ConduitIO/conduit#576) describes a cross-cutting concern (error handling spans many packages), which means even a "low blast radius" classification may underestimate the coordination complexity. This suggests the L1 difficulty classification may warrant a qualifier for tasks that require multi-package consistency, regardless of the conceptual simplicity of the fix.

## Verdict

**H2 (agents perform best on low-risk tasks): Partially refuted for this task type.** The agent demonstrated correct architectural intent on an L1/low task, but failed mechanically due to cross-file naming inconsistency. The failure is *not* conceptual (the agent understood the problem and the right approach) but *structural* (the per-file generation architecture cannot maintain naming consistency across files). This suggests that H2 needs a qualifier: agents perform best on low-risk tasks *that don't require tight coordination across multiple files*. Tasks that are conceptually simple but structurally cross-cutting may exceed current capability even at L1.

**H3 (architectural review adds value): Strongly supported.** The architect produced three substantive findings: (1) correctly identified the build failure, (2) affirmed the architectural intent as sound, and (3) identified a deeper semantic flaw that would survive a build-fix-only repair. Finding (3) is particularly significant because it demonstrates the architect catching an issue that would lead to a `semantically_incorrect_fix` if the build were naively fixed. A single coding agent without architectural review would have either stopped at the build error (missing the semantic flaw) or, if it fixed the naming, would have shipped a change that compiles but doesn't actually solve the bug. The architect role provided genuine review value that goes beyond what verification alone could surface.

## Limitations

- **n=1.** A single run on a single cross-cutting task does not prove that all multi-file tasks will exhibit cross-file inconsistency. The failure is an artifact of the current per-file generation architecture and may not occur on tasks where the plan touches fewer files or where the files don't share symbols.
- **No iteration.** The pipeline currently does not iterate: the architect recommends `revise`, but this feedback is recorded rather than acted upon. A single round of "architect says fix X, implementer retries" might resolve the naming inconsistency, since the architect's feedback implicitly identifies the mismatch. We cannot assess whether iteration would have salvaged this run.
- **Retrofitted prediction.** The nuanced prediction (H2 should succeed on difficulty but may fail on breadth) was written after observing results. The honest pre-run prediction for an L1/low task would have been "expect success." The breadth concern was not anticipated.
- **LLM non-determinism.** The naming inconsistency may not reproduce on every run. A different random seed could produce consistent names. We do not know the reproduction rate of this failure mode.
- **The deeper semantic flaw is unverified.** The architect's point about `cerrors.Is` not matching runtime errors is an LLM-generated claim, not a verified fact. It is plausible given Conduit's error-handling patterns, but we have not independently verified that existing validation errors fail to match the proposed sentinels. This claim should be treated as a hypothesis for future investigation, not established fact.

## References

- Run artifacts: `data/runs/run-task-gh-576-20260405-151540/` (run.json, dossier.json, evaluation.json, report.md)
- Task definition: `data/tasks/task-gh-576.json`
- Original issue: ConduitIO/conduit#576
- Build error site: `pkg/http/api/status/status.go:82-84` in the generated worktree
- Error definitions (agent-generated): `pkg/foundation/cerrors/errors.go` in the generated worktree
- Cross-file generation code: `internal/orchestrator/workflow.go:195,220` (per-file `GenerateFileContent` loop)
- Implementer agent: `internal/agents/implementer.go:75-101` (`GenerateFileContent`)
- PRD hypotheses: `docs/design.md` section 7.9
- PRD failure taxonomy: `docs/design.md` section 7.18
