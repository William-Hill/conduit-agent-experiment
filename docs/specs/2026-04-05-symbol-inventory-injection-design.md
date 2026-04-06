# Phase 1 Design: Package + Error Sentinel Inventory for Implementer Context

**Status:** Approved
**Date:** 2026-04-05
**Motivation:** Experiment 04 failed because the implementer imported `github.com/conduitio/conduit/pkg/config`, a package that doesn't exist. The agent had no information about which packages or error sentinels actually exist in the target repo. The symbol extractor (`internal/ingest/symbol_extractor.go`) already parses the full Go AST but is not wired into the pipeline.

## Goal

Inject a compact package + error sentinel inventory into the implementer's generation prompt so the LLM knows which packages exist and which error constants are available, preventing hallucinated imports and symbol references.

## Architecture

Call `BuildSymbolIndex` during the ingest phase of the workflow (alongside the existing `WalkRepo` call). Filter the index to extract a compact inventory: package paths plus exported error sentinels (`var Err* = ...`). Store the inventory on the dossier. Include it in the implementer's `buildFileContentPrompt` so every `GenerateFileContent` and `ReviseFileContent` call sees what actually exists in the target repo.

## Components

### 1. Inventory generation

New function in `internal/ingest/symbol_extractor.go`:

```go
func BuildPackageInventory(idx *SymbolIndex) map[string][]string
```

Returns `package import path → []error sentinel names`. Filters to symbols where `Kind == "var"`, `Exported == true`, and name starts with `Err`. Packages with no error sentinels are included with an empty slice (so the implementer can still see they exist) but are rendered differently in the prompt.

### 2. Dossier enrichment

Add field to `internal/models/dossier.go`:

```go
PackageInventory map[string][]string `json:"package_inventory,omitempty"`
```

Populated in `workflow.go` after `BuildSymbolIndex` is called, before the archivist enhancement step.

### 3. Prompt injection

Update `buildFileContentPrompt` in `internal/agents/implementer.go` to accept the inventory (via the dossier or directly) and render it as:

```
## Available Packages and Error Sentinels
IMPORTANT: Only import packages listed below. Do not invent package paths.

pkg/foundation/cerrors: ErrNotImpl, ErrEmptyID
pkg/connector: ErrInvalidConnectorType, ErrConnectorRunning, ErrInstanceNotFound, ErrNameOverLimit, ErrNameMissing, ErrIDMissing
pkg/pipeline: ErrPipelineRunning, ErrPipelineNotRunning, ErrNameAlreadyExists, ErrNameMissing, ErrDescriptionOverLimit, ...
pkg/orchestrator: ErrPipelineHasConnectorsAttached, ErrPipelineHasProcessorsAttached, ...
pkg/http/api/status: (no error sentinels)
...
```

The section is omitted entirely when `PackageInventory` is nil or empty (non-Go repos, or if `BuildSymbolIndex` fails).

### 4. Workflow wiring

In `internal/orchestrator/workflow.go`, after `WalkRepo` and before `BuildDossier`:

```go
symbolIdx, err := ingest.BuildSymbolIndex(cfg.Target.RepoPath)
// non-fatal: if symbol extraction fails, just skip the inventory
inventory := ingest.BuildPackageInventory(symbolIdx)
```

Then pass `inventory` into the dossier (either via `BuildDossier` parameter or by setting `dossier.PackageInventory` directly after `BuildDossier` returns).

## Data Flow

```
WalkRepo → BuildSymbolIndex → BuildPackageInventory → dossier.PackageInventory
  → buildFileContentPrompt → "## Available Packages and Error Sentinels"
  → GenerateFileContent / ReviseFileContent
```

## Scope Exclusions (YAGNI)

- No full symbol index in the prompt — only error sentinels and package existence
- No active tool use — that's Phase 2 (ADK migration)
- No changes to archivist or architect prompts — they don't generate code
- No caching or persistence of the symbol index — rebuilt per run (<1s for Conduit)
- No config flag to enable/disable — always generated for Go repos, omitted for non-Go

## Testing

1. **Unit test for `BuildPackageInventory`** — given a `SymbolIndex` with mixed symbols (funcs, types, Err* vars, non-exported vars), returns only exported `Err*` vars grouped by package
2. **Unit test for prompt rendering** — verify the "Available Packages" section appears when inventory is populated, and is omitted when nil/empty
3. **Integration validation** — re-run task-gh-576 after implementation to verify the agent no longer imports `pkg/config`

## Expected Impact

For task-gh-576 specifically: the implementer would see that `pkg/config` does not exist in the inventory, and that error sentinels like `ErrDuplicateID` are not defined in any package. The agent should use only existing packages and existing error constants, eliminating the hallucinated import that caused the experiment 04 build failure.
