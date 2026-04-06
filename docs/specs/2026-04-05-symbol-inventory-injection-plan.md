# Symbol Inventory Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Inject a package + error sentinel inventory into the implementer prompt so the LLM knows which packages and error constants actually exist, preventing hallucinated imports.

**Architecture:** `BuildSymbolIndex` (already exists) runs at ingest time. A new `BuildPackageInventory` function filters to error sentinels and keys by full import path. The inventory is stored on the Dossier and rendered in `buildFileContentPrompt`. No new dependencies.

**Tech Stack:** Go 1.24, `go/ast` (existing), `go/parser` (existing)

---

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/ingest/symbol_extractor.go` | Add `BuildPackageInventory` function | Filter symbols to error sentinels, key by import path |
| `internal/ingest/symbol_extractor_test.go` | Add test for `BuildPackageInventory` | Verify filtering and import path construction |
| `internal/models/dossier.go` | Add `PackageInventory` field | Store inventory on dossier |
| `internal/agents/implementer.go` | Update `buildFileContentPrompt` | Render inventory in prompt |
| `internal/agents/implementer_test.go` | Add test for inventory in prompt | Verify section renders/omits correctly |
| `internal/orchestrator/workflow.go` | Call `BuildSymbolIndex` + `BuildPackageInventory`, set on dossier | Wire into pipeline |

---

### Task 1: BuildPackageInventory Function

**Files:**
- Modify: `internal/ingest/symbol_extractor.go`
- Test: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write the test for BuildPackageInventory**

In `internal/ingest/symbol_extractor_test.go`, add:

```go
func TestBuildPackageInventory(t *testing.T) {
	// Reuse the test repo from setupSymbolTestRepo.
	root := setupSymbolTestRepo(t)
	idx, err := BuildSymbolIndex(root)
	if err != nil {
		t.Fatalf("BuildSymbolIndex error: %v", err)
	}

	inventory := BuildPackageInventory(idx, root)

	// The test repo has package "models" with no Err* vars,
	// and package "errors" with ErrNotFound.
	// Check that error sentinels are captured.
	found := false
	for dir, sentinels := range inventory {
		for _, s := range sentinels {
			if strings.HasPrefix(s, "Err") {
				found = true
			}
			_ = dir
		}
	}
	// The setupSymbolTestRepo defines ErrNotFound in errors/errors.go.
	if !found {
		t.Error("expected at least one Err* sentinel in inventory")
	}

	// Verify packages without error sentinels still appear (with empty slice).
	hasEmptyPkg := false
	for _, sentinels := range inventory {
		if len(sentinels) == 0 {
			hasEmptyPkg = true
		}
	}
	if !hasEmptyPkg {
		t.Error("expected at least one package with no error sentinels")
	}
}
```

Note: `setupSymbolTestRepo` already creates a test repo with `errors/errors.go` (containing `var ErrNotFound = errors.New("not found")`) and `models/task.go` (containing types, no Err vars). Read the existing helper to confirm the exact file content, and adjust assertions if names differ.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/ingest/ -v -run TestBuildPackageInventory
```

Expected: FAIL — `BuildPackageInventory` is undefined.

- [ ] **Step 3: Implement BuildPackageInventory**

In `internal/ingest/symbol_extractor.go`, add after `SearchSymbols`:

```go
// BuildPackageInventory extracts a compact inventory of packages and their
// exported error sentinels from a SymbolIndex. Keys are directory paths
// relative to the repo root (e.g., "pkg/foundation/cerrors"). Values are
// slices of exported Err* variable names. Packages with no error sentinels
// are included with an empty slice so the LLM can see they exist.
func BuildPackageInventory(idx *SymbolIndex, repoPath string) map[string][]string {
	if idx == nil {
		return nil
	}

	// Group error sentinels by directory (not short package name,
	// since multiple packages can share a short name).
	dirSentinels := make(map[string][]string)
	dirSeen := make(map[string]bool)

	for _, s := range idx.Symbols {
		dir := filepath.Dir(s.File)
		if !dirSeen[dir] {
			dirSeen[dir] = true
			dirSentinels[dir] = nil // ensure the key exists even if no sentinels
		}
		if s.Kind == "var" && s.Exported && strings.HasPrefix(s.Name, "Err") {
			dirSentinels[dir] = append(dirSentinels[dir], s.Name)
		}
	}

	return dirSentinels
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/ingest/ -v -run TestBuildPackageInventory
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "feat: add BuildPackageInventory for error sentinel extraction

Filters SymbolIndex to exported Err* variables grouped by directory
path. Packages without error sentinels are included with empty slices
so the LLM can see which packages exist."
```

---

### Task 2: Wire Inventory into Dossier and Implementer Prompt

**Files:**
- Modify: `internal/models/dossier.go`
- Modify: `internal/agents/implementer.go:179-206`
- Test: `internal/agents/implementer_test.go`

- [ ] **Step 1: Add PackageInventory field to Dossier**

In `internal/models/dossier.go`, add after `OpenQuestions`:

```go
// PackageInventory maps directory paths to exported error sentinel names.
// Used by the implementer to know which packages and error constants exist.
PackageInventory map[string][]string `json:"package_inventory,omitempty"`
```

- [ ] **Step 2: Write the test for inventory in prompt**

In `internal/agents/implementer_test.go`, add:

```go
func TestBuildFileContentPromptWithInventory(t *testing.T) {
	plan := PatchPlan{PlanSummary: "Fix error handling"}
	task := models.Task{ID: "test", Title: "test task", Description: "test"}
	inventory := map[string][]string{
		"pkg/foundation/cerrors": {"ErrNotImpl", "ErrEmptyID"},
		"pkg/connector":         {"ErrInvalidConnectorType", "ErrConnectorRunning"},
		"pkg/http/api/status":   {},
	}
	prompt := buildFileContentPrompt(plan, task, "pkg/handler.go", "package handler", nil, inventory)
	if !strings.Contains(prompt, "Available Packages and Error Sentinels") {
		t.Error("prompt should contain inventory section")
	}
	if !strings.Contains(prompt, "pkg/foundation/cerrors") {
		t.Error("prompt should contain cerrors package path")
	}
	if !strings.Contains(prompt, "ErrNotImpl") {
		t.Error("prompt should contain ErrNotImpl sentinel")
	}
	if !strings.Contains(prompt, "Only import packages listed below") {
		t.Error("prompt should contain import restriction instruction")
	}
}

func TestBuildFileContentPromptNoInventory(t *testing.T) {
	plan := PatchPlan{PlanSummary: "Simple change"}
	task := models.Task{ID: "test", Title: "test task", Description: "test"}
	prompt := buildFileContentPrompt(plan, task, "pkg/foo.go", "package foo", nil, nil)
	if strings.Contains(prompt, "Available Packages") {
		t.Error("prompt should NOT contain inventory section when nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/agents/ -v -run "TestBuildFileContentPrompt(With|No)Inventory"
```

Expected: FAIL — `buildFileContentPrompt` doesn't accept an inventory parameter yet.

- [ ] **Step 4: Update buildFileContentPrompt to accept and render inventory**

In `internal/agents/implementer.go`, update the function signature at line 179:

```go
func buildFileContentPrompt(plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, packageInventory map[string][]string) string {
```

Add the inventory section after the sibling contents section and before "## File to Generate" (before line 198). Insert:

```go
// Package inventory for import validation.
if len(packageInventory) > 0 {
	fmt.Fprintf(&b, "## Available Packages and Error Sentinels\n")
	fmt.Fprintf(&b, "IMPORTANT: Only import packages listed below. Do not invent package paths or error constant names.\n\n")
	// Sort keys for deterministic output.
	dirs := make([]string, 0, len(packageInventory))
	for dir := range packageInventory {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		sentinels := packageInventory[dir]
		if len(sentinels) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", dir, strings.Join(sentinels, ", "))
		} else {
			fmt.Fprintf(&b, "%s: (no error sentinels)\n", dir)
		}
	}
	fmt.Fprintf(&b, "\n")
}
```

Add `"sort"` to the imports at the top of `implementer.go` if not already present.

- [ ] **Step 5: Update all callers of buildFileContentPrompt**

`buildFileContentPrompt` is called in three places:
1. `GenerateFileContent` (line ~79) — pass `dossier.PackageInventory` (but `GenerateFileContent` doesn't currently receive the dossier). Instead, add `packageInventory map[string][]string` as a parameter to `GenerateFileContent`.
2. `ReviseFileContent` (line ~108) — same: add `packageInventory` parameter.
3. Existing tests — update to pass `nil` for the new parameter.

Update `GenerateFileContent` signature:

```go
func GenerateFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, packageInventory map[string][]string) (string, models.LLMCall, error) {
```

And its internal call:
```go
userPrompt := buildFileContentPrompt(plan, task, filePath, currentContent, siblingContents, packageInventory)
```

Update `ReviseFileContent` signature similarly:

```go
func ReviseFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, architectFeedback string, packageInventory map[string][]string) (string, models.LLMCall, error) {
```

And its internal prompt construction:
```go
userPrompt := buildFileContentPrompt(plan, task, filePath, currentContent, siblingContents, packageInventory)
```

Update all callers in `workflow.go` — every call to `GenerateFileContent` and `ReviseFileContent` needs `dossier.PackageInventory` as the last argument.

Update existing tests in `implementer_test.go` that call `GenerateFileContent`, `ReviseFileContent`, or `buildFileContentPrompt` to pass `nil` for the new inventory parameter.

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/agents/ ./internal/orchestrator/ -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/models/dossier.go internal/agents/implementer.go internal/agents/implementer_test.go internal/orchestrator/workflow.go
git commit -m "feat: render package inventory in implementer prompt

buildFileContentPrompt now accepts a package inventory and renders it
as 'Available Packages and Error Sentinels' with an instruction to
only import listed packages. GenerateFileContent and ReviseFileContent
pass the inventory through from the dossier."
```

---

### Task 3: Wire BuildSymbolIndex into Workflow

**Files:**
- Modify: `internal/orchestrator/workflow.go:76-81`

- [ ] **Step 1: Add symbol index + inventory call to workflow**

In `internal/orchestrator/workflow.go`, after the `WalkRepo` call (line 77) and before `BuildDossier` (line 81), add:

```go
// Build symbol index for package inventory (non-fatal on failure).
var packageInventory map[string][]string
symbolIdx, symErr := ingest.BuildSymbolIndex(cfg.Target.RepoPath)
if symErr != nil {
	log.Printf("symbol index failed (continuing without inventory): %v", symErr)
} else {
	packageInventory = ingest.BuildPackageInventory(symbolIdx, cfg.Target.RepoPath)
}
```

After `BuildDossier` (line 81), set the inventory on the dossier:

```go
dossier := retrieval.BuildDossier(task, inv)
dossier.PackageInventory = packageInventory
```

Make sure `ingest` is in the imports (it should already be — `ingest.WalkRepo` is called above).

- [ ] **Step 2: Run full test suite**

```bash
go build ./... && go test ./internal/... -v
```

Expected: BUILD OK, all tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/workflow.go
git commit -m "feat: wire BuildSymbolIndex into workflow pipeline

Calls BuildSymbolIndex at ingest time, extracts package inventory,
and stores it on the dossier. Non-fatal: if symbol extraction fails,
the pipeline continues without the inventory."
```

---

## Self-Review

**Spec coverage:**
- Inventory generation (spec component 1) → Task 1 (`BuildPackageInventory`)
- Dossier enrichment (spec component 2) → Task 2 step 1 (`PackageInventory` field)
- Prompt injection (spec component 3) → Task 2 steps 4-5 (`buildFileContentPrompt` update)
- Workflow wiring (spec component 4) → Task 3
- Testing → Tasks 1 and 2 include unit tests; integration validation is manual (re-run task-gh-576)
- All spec components covered.

**Placeholder scan:** No TBDs, TODOs, or vague instructions. All code blocks are complete.

**Type consistency:**
- `BuildPackageInventory` returns `map[string][]string` in Task 1; `PackageInventory` field is `map[string][]string` in Task 2; `buildFileContentPrompt` accepts `map[string][]string` in Task 2. Consistent.
- `GenerateFileContent` gains `packageInventory map[string][]string` in Task 2; callers in `workflow.go` updated in Task 2 step 5 and Task 3. Consistent.
- `ReviseFileContent` gains `packageInventory map[string][]string` in Task 2; callers in workflow revision loop updated in Task 2 step 5. Consistent.
