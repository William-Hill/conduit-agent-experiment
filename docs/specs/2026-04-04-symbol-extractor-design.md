# Symbol Extractor Design Spec

**Issue:** #7 — Symbol Extractor: Go AST parsing for improved dossier context
**Date:** 2026-04-04
**Branch:** off `main`
**Scope:** `internal/ingest/symbol_extractor.go` + `internal/ingest/symbol_extractor_test.go`

## Goal

Parse Go source files using `go/ast` and extract structured symbol information (functions, methods, types, interfaces, constants, variables). This gives the Archivist and Implementer concrete knowledge of function signatures, type definitions, and package structure — improving dossier quality.

## Data Model

```go
type Symbol struct {
    Name      string // e.g. "RunWorkflow"
    Kind      string // "func", "type", "interface", "method", "const", "var"
    Package   string // e.g. "orchestrator"
    File      string // relative path
    Line      int
    Signature string // e.g. "func RunWorkflow(ctx context.Context) error"
    Doc       string // godoc comment
    Exported  bool
    Receiver  string // for methods, e.g. "Policy"
}

type SymbolIndex struct {
    Symbols   []Symbol
    ByPackage map[string][]Symbol
    ByKind    map[string][]Symbol
    ByFile    map[string][]Symbol
}
```

`SymbolIndex` maps are built once by `BuildSymbolIndex` in a single grouping loop after all symbols are collected. No lazy population.

## Public API

All functions live in `internal/ingest/symbol_extractor.go`.

### `ExtractSymbols(filePath string) ([]Symbol, error)`

Parses a single `.go` file with `go/parser.ParseFile` (with `parser.ParseComments` for godoc). Uses a single `ast.Inspect` pass over the AST.

Handles:
- `*ast.FuncDecl` — functions and methods
- `*ast.GenDecl` with `token.TYPE` — types and interfaces
- `*ast.GenDecl` with `token.CONST` — constants
- `*ast.GenDecl` with `token.VAR` — variables

Signature rendering uses string building from field lists (params, results, receiver). No `go/format` or `go/printer` dependency.

Package name comes from `file.Name.Name`. `ExtractSymbols` does not set the `File` field (it only knows the absolute path, not the repo-relative path). The caller (`BuildSymbolIndex`) populates `File` with the relative path after calling `ExtractSymbols`.

### `BuildSymbolIndex(repoPath string, opts ...IndexOption) (*SymbolIndex, error)`

Walks the repo with `filepath.WalkDir`, reuses `skipDir()` from `repo_loader.go` to skip `.git`, `vendor`, `node_modules`, `bin`, `dist`.

Filters to `.go` files only. By default, skips `_test.go` files. The `WithTests()` option includes them.

Calls `ExtractSymbols` per file, sets `File` to the relative path. Builds the three index maps in a single pass after collecting all symbols.

#### Options

```go
type IndexOption func(*indexConfig)
type indexConfig struct {
    includeTests bool
}
func WithTests() IndexOption {
    return func(c *indexConfig) { c.includeTests = true }
}
```

### `SearchSymbols(index *SymbolIndex, query string) []Symbol`

Case-insensitive substring match against `Name`, `Signature`, and `Doc`. Returns all matches, no scoring or ranking.

### `FormatSymbolContext(symbols []Symbol) string`

Formats symbols as readable text for LLM prompts. Groups by package, renders each symbol as its signature with doc if present. Returns a single string ready to embed in a prompt.

## AST Extraction Detail

The `ast.Inspect` callback handles four node types:

- **`*ast.FuncDecl`** — `Recv == nil` means `"func"`, otherwise `"method"`. Receiver type name extracted from the receiver field list, stripping pointer `*` if present. Signature built from receiver + name + params + results.

- **`*ast.GenDecl` / `token.TYPE`** — Each `*ast.TypeSpec` becomes a symbol. Kind is `"interface"` if the underlying type is `*ast.InterfaceType`, otherwise `"type"`. Signature is `type Name <underlying>` (e.g., `type FileCategory string`, `type Processor interface`).

- **`*ast.GenDecl` / `token.CONST`** — Each `*ast.ValueSpec` becomes a `"const"` symbol. Signature includes the type if explicit.

- **`*ast.GenDecl` / `token.VAR`** — Same pattern as const, kind is `"var"`.

An unexported helper `formatFieldList(*ast.FieldList) string` renders field lists as `name Type, name Type` — used for func params, results, and interface method lists. This avoids importing `go/format` or `go/printer`.

Doc comments come from `node.Doc.Text()` (trimmed). Both `FuncDecl.Doc` and `GenDecl.Doc` / `TypeSpec.Doc` are checked.

`Exported` is `token.IsExported(name)`.

## Error Handling

- `ExtractSymbols` returns an error if `parser.ParseFile` fails (syntax errors in the target file).
- `BuildSymbolIndex` skips files that fail to parse and continues walking. Only returns an error if the walk itself fails (e.g., root directory doesn't exist).

## Testing Strategy

Tests in `internal/ingest/symbol_extractor_test.go`, following the pattern in `repo_loader_test.go`.

- **`TestExtractSymbols`** — Write a temp `.go` file with known symbols (func, method, type, interface, const, var), parse it, assert each symbol's fields.
- **`TestExtractSymbols_Signature`** — Signature rendering accuracy for various function shapes (multi-param, multi-return, pointer receiver).
- **`TestExtractSymbols_Doc`** — Verify godoc comment extraction.
- **`TestBuildSymbolIndex`** — Create a temp repo tree, verify index maps are populated correctly, verify test files excluded by default.
- **`TestBuildSymbolIndex_WithTests`** — Same tree, verify `_test.go` symbols included.
- **`TestSearchSymbols`** — Build an index, search by name/signature/doc substring, verify matches.
- **`TestFormatSymbolContext`** — Verify output string structure.

All tests use `t.TempDir()` with synthesized `.go` files. No dependency on the real repo.

## Integration Point

Not wired into `BuildDossier` in this PR. The extractor is shipped standalone. A follow-up can add a `SymbolContext` field to `Dossier` and call `BuildSymbolIndex` + `SearchSymbols` + `FormatSymbolContext` from `BuildDossier`.

## Constraints

- Only stdlib: `go/ast`, `go/parser`, `go/token` — no external dependencies
- Only touches `internal/ingest/` — no overlap with Milestone 2 work
- TDD: tests first
