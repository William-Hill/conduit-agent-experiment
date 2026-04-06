# Experiment 05: HTTP Status Codes with Package Inventory (Build Passes)

- **Date:** 2026-04-06
- **Task ID:** task-gh-576 (third run of this task)
- **Source:** GitHub issue ConduitIO/conduit#576
- **Difficulty:** L1
- **Blast Radius:** low
- **Run ID:** run-task-gh-576-20260406-103050
- **Duration:** 557 seconds
- **LLM Calls:** 14 (1 archivist, 3 implementer [1 plan + 2 file generations], 1 architect [round 1], 2 implementer-revise, 1 architect [round 2], plus retries)
- **Final Status:** failed
- **Architect Recommendation:** revise (high confidence)
- **Revisions:** 1
- **Packages in inventory:** 96

## Introduction

This is the third run of task-gh-576 and the first with the package inventory (Phase 1) active. The inventory injects a list of all packages and their exported error sentinels into the implementer prompt, telling the LLM what actually exists in the target repository.

Previous runs:
- **Experiment 02** (no fixes): Build failed — cross-file naming inconsistency. 10 files, 13 LLM calls.
- **Experiment 04** (pipeline fixes, no inventory): Build failed — hallucinated import (`pkg/config`). 2 files, 8 LLM calls.
- **Experiment 05** (this run, all fixes + inventory): **Build passes.** Test syntax error. 5 files, 14 LLM calls.

## Hypothesis

**Primary:** The package inventory (96 packages indexed) prevents hallucinated imports, allowing `go build` to pass.

**Secondary:** H2 — with sufficient context, L1 tasks should be within agent capability.

## Prediction (retrofitted)

With the inventory showing exactly which packages and error sentinels exist, the agent should no longer import nonexistent packages. `go build` should pass. Whether `go test` passes depends on the quality of generated test code, which the inventory does not directly help with.

## Method

Same task definition and verifier commands as experiments 02 and 04. New: `dossier.PackageInventory` populated with 96 directory paths and their exported `Err*` variables, injected into every `GenerateFileContent` and `ReviseFileContent` call as an "Available Packages and Error Sentinels" prompt section.

Configuration: `max_revisions: 1`, all other settings identical to experiment 04.

## Results

### Baseline Verifier

| Command | Baseline Exit Code | Classification |
|---------|-------------------|----------------|
| `go build ./...` | 0 | clean |
| `go vet ./...` | 1 | **environment** (pre-existing) |
| `go test ./pkg/http/api/...` | 0 | clean |

### Post-Patch Verifier

| Command | Exit Code | Classification | Comparison to exp 02/04 |
|---------|-----------|----------------|------------------------|
| `go build ./...` | **0** | **PASS** | Was FAIL in both prior runs |
| `go vet ./...` | 1 | environment | Same as baseline |
| `go test ./pkg/http/api/...` | 1 | **patch-caused** | New failure mode |

**`go build` passes for the first time on this task across all experiments.** The hallucinated import that killed experiments 02 and 04 is gone. The agent used only real packages from the inventory.

The `go test` failure is a syntax error in agent-generated test code:
```
pkg/http/api/connector_v1_test.go:166:34: expected '{', found newline
```

A missing opening brace in a test function — a minor generation bug, not a knowledge or architecture problem.

### Files Changed

5 files (up from 2 in experiment 04, down from 10 in experiment 02):
- `pkg/http/api/status/status.go` — extended `codeFromError` with comprehensive validation mappings
- `pkg/http/api/status/status_test.go` — extensive new test cases
- `pkg/http/api/connector_v1_test.go` — new validation error test (contains syntax error)
- `pkg/http/api/pipeline_v1_test.go` — new validation error test
- `proto/api/v1/api.proto` — updated OpenAPI error response documentation

### Architect Review

> "The patch introduces valuable improvements to the API's error handling by replacing generic HTTP 500 errors with semantically appropriate 400-level status codes and updating the OpenAPI specification. This is a significant step forward for client usability and API clarity. However, the reported failure in the `go test ./pkg/http/api/...` suite is a critical blocker."

The architect correctly identified the test failure as the blocking issue and acknowledged the architectural intent as sound.

### Revision Loop

The revision loop fired (round 1/1). The implementer regenerated files with architect feedback, but the test syntax error persisted after revision. The revision loop cannot fix syntax errors it cannot see — it receives the architect's high-level feedback ("test failure is a critical blocker") but not the specific compiler error (`expected '{', found newline`).

## Analysis

### The progression across three runs of the same task

| Run | Build | Files | Root cause | What fixed it |
|-----|-------|-------|-----------|---------------|
| Exp 02 (no fixes) | FAIL | 10 | Cross-file naming inconsistency | Sibling contents |
| Exp 04 (pipeline fixes) | FAIL | 2 | Hallucinated import (`pkg/config`) | Package inventory |
| **Exp 05 (+ inventory)** | **PASS** | 5 | Test syntax error (missing `{`) | **Next: compile-check loop** |

Each fix eliminated a class of failures and revealed the next, simpler layer:
1. **Layer 1 (mechanical coordination):** Files generated independently chose different names. Fixed by sharing sibling content.
2. **Layer 2 (knowledge gap):** Agent imported packages that don't exist. Fixed by providing a package inventory.
3. **Layer 3 (syntax correctness):** Generated test code has a missing brace. Requires a compile-check feedback loop.

### Why this experiment keeps failing while Claude Code succeeds

The most important observation from this experiment series is not about any individual failure — it's about the structural difference between the experiment's agent architecture and a tool-using agent like Claude Code.

Claude Code succeeds on tasks like this because it operates as a **tool-using agent with iterative feedback loops**:

1. **Active context acquisition:** Claude Code reads files on demand. Before importing a package, it can `Glob("pkg/config")` to check if it exists. The experiment's agent gets a static prompt and cannot ask questions.

2. **Compile-check iteration:** Claude Code writes code, runs `go build`, sees the error, and fixes it — often multiple times per file. The experiment's agent generates all files in one shot and never sees build errors during generation.

3. **Multi-turn reasoning:** Claude Code can read file A, think about it, then read file B before writing code. The experiment's agent gets one prompt and returns one response.

4. **Tool-mediated verification:** Claude Code runs tests after each change and reads the output. The experiment's verifier runs once after all generation is complete, and the errors go to the architect (a separate LLM call) rather than back to the implementer.

Each failure mode in experiments 02-05 maps to a missing capability:

| Failure | What Claude Code would do | What the experiment lacks |
|---------|--------------------------|--------------------------|
| Cross-file naming (exp 02) | Read the file it just wrote | Active file access mid-generation |
| Hallucinated import (exp 04) | `Glob("pkg/config")` → doesn't exist | Tool use for validation |
| Test syntax error (exp 05) | `go build` → see error → fix brace | Compile-check feedback loop |

The passive fixes built so far (sibling contents, package inventory) are **workarounds for the lack of tool use** — they inject context the agent would have found on its own if it could read files. Each fix makes the static prompt closer to what an interactive agent would discover, but it's fundamentally limited because we're predicting what the agent needs rather than letting it ask.

**The limiting factor is not model capability — it's agent architecture.** The same LLM with tools and iteration would likely succeed. This is the motivation for Phase 2 (ADK migration): ADK's `functiontool` gives the agent `list_packages()`, `lookup_symbol()`, `read_file()` mid-generation. The `loopagent` gives it iterative generate-build-fix cycles. These aren't nice-to-haves — they're the structural difference between "generate and hope" and "generate, verify, fix."

### Failure classification

**Primary:** The test syntax error is `implementation_hallucination` in the existing taxonomy — the agent generated syntactically invalid Go code. However, this is the mildest form of hallucination: a missing brace, not a wrong algorithm or nonexistent API. A single `go build` feedback cycle would fix it.

### What the agent did well

1. **No hallucinated imports.** The 96-package inventory worked — every import in the generated code references a real Conduit package.
2. **Comprehensive error mapping.** The `codeFromError` rewrite maps NotFound, AlreadyExists, InvalidArgument, FailedPrecondition, and Unimplemented correctly with well-organized case groups.
3. **Updated OpenAPI spec.** The agent modified `proto/api/v1/api.proto` to document the 400 error response — going beyond code to update API documentation.
4. **Extensive test coverage.** 20+ new test cases for specific error-to-status-code mappings (would pass if the syntax error were fixed).

## Verdict

**Package inventory: Validated.** The hallucinated import class of failures is eliminated. `go build` passes for the first time on this task.

**H2 (agents on low-risk tasks): Progress, not yet success.** The agent is one syntax fix away from a passing build + tests. The architectural intent is correct, the package references are real, the test coverage is comprehensive. The remaining failure (missing brace) is the kind of error a compile-check loop would catch in seconds.

**Architecture insight (most important finding):** The experiment's architecture — single-shot generation with passive context — hits diminishing returns. Each passive fix (siblings, inventory) eliminates one failure class but cannot prevent the next. An active, tool-using architecture (like Claude Code or ADK) would prevent all three failure classes simultaneously by letting the agent check its work during generation, not after.

## Limitations

- **LLM non-determinism.** The syntax error may not reproduce. A different run might generate valid test code.
- **Inventory quality.** The inventory shows Err* vars only. Non-error symbols (types, funcs) are not included. Tasks requiring type references could still hallucinate.
- **Retrofitted prediction.** The prediction that build would pass was made with direct knowledge of the inventory fix targeting exactly this failure.

## References

- Run artifacts: `data/runs/run-task-gh-576-20260406-103050/`
- Comparison runs: `data/runs/run-task-gh-576-20260405-151540/` (exp 02), `data/runs/run-task-gh-576-20260405-212335/` (exp 04)
- Task definition: `data/tasks/task-gh-576.json`
- Package inventory source: `internal/ingest/symbol_extractor.go:BuildPackageInventory`
- Dossier inventory field: `internal/models/dossier.go:PackageInventory`
- Phase 2 motivation: ADK Go `functiontool` for active context acquisition
