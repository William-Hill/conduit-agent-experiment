# Symbol Extractor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Parse Go source files with `go/ast` to extract structured symbol information (functions, methods, types, interfaces, constants, variables) for improved dossier context.

**Architecture:** Single-pass `ast.Inspect` per file, functional options for `BuildSymbolIndex`, all code in one file (`internal/ingest/symbol_extractor.go`) with tests alongside. No external dependencies — stdlib `go/ast`, `go/parser`, `go/token` only.

**Tech Stack:** Go 1.24, `go/ast`, `go/parser`, `go/token`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/ingest/symbol_extractor.go` | `Symbol`, `SymbolIndex` types, `ExtractSymbols`, `BuildSymbolIndex`, `SearchSymbols`, `FormatSymbolContext`, helpers |
| `internal/ingest/symbol_extractor_test.go` | All tests for the above |

---

### Task 1: Create branch and scaffold types + options

**Files:**
- Modify: `internal/ingest/symbol_extractor.go` (currently just `package ingest`)

- [ ] **Step 1: Create feature branch from main**

```bash
git checkout main
git checkout -b feature/symbol-extractor
```

- [ ] **Step 2: Write the types and options into symbol_extractor.go**

Replace the contents of `internal/ingest/symbol_extractor.go` with:

```go
package ingest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Symbol represents a single extracted Go symbol.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`      // "func", "type", "interface", "method", "const", "var"
	Package   string `json:"package"`
	File      string `json:"file"`      // relative path
	Line      int    `json:"line"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
	Exported  bool   `json:"exported"`
	Receiver  string `json:"receiver"`  // for methods only
}

// SymbolIndex is a collection of symbols with lookup maps.
type SymbolIndex struct {
	Symbols   []Symbol            `json:"symbols"`
	ByPackage map[string][]Symbol `json:"by_package"`
	ByKind    map[string][]Symbol `json:"by_kind"`
	ByFile    map[string][]Symbol `json:"by_file"`
}

// IndexOption configures BuildSymbolIndex behavior.
type IndexOption func(*indexConfig)

type indexConfig struct {
	includeTests bool
}

// WithTests includes _test.go files in the symbol index.
func WithTests() IndexOption {
	return func(c *indexConfig) { c.includeTests = true }
}
```

Note: the imports will be used by subsequent tasks. The Go compiler will complain about unused imports if you build now — that's expected and resolved in Task 2.

- [ ] **Step 3: Commit**

```bash
git add internal/ingest/symbol_extractor.go
git commit -m "feat: add Symbol and SymbolIndex types with IndexOption"
```

---

### Task 2: ExtractSymbols — test and implement

**Files:**
- Create: `internal/ingest/symbol_extractor_test.go`
- Modify: `internal/ingest/symbol_extractor.go`

- [ ] **Step 1: Write TestExtractSymbols**

Create `internal/ingest/symbol_extractor_test.go`:

```go
package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

const testGoSource = `package example

import "context"

// StatusCode represents an HTTP status.
type StatusCode int

// Handler processes requests.
type Handler interface {
	Handle(ctx context.Context) error
}

// DefaultTimeout is the default timeout.
const DefaultTimeout = 30

const (
	// ModeRead is read mode.
	ModeRead string = "read"
	// ModeWrite is write mode.
	ModeWrite string = "write"
)

// globalLogger is the package-level logger.
var globalLogger = "default"

// Run executes the main workflow.
func Run(ctx context.Context, name string) (*Result, error) {
	return nil, nil
}

type Result struct {
	Value int
}

// Process handles a single item.
func (r *Result) Process(input string) (string, bool) {
	return input, true
}

func unexportedHelper() {}
`

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractSymbols(t *testing.T) {
	path := writeTestFile(t, testGoSource)

	symbols, err := ExtractSymbols(path)
	if err != nil {
		t.Fatalf("ExtractSymbols() error: %v", err)
	}

	// Build a map by name for easier assertions.
	byName := make(map[string]Symbol)
	for _, s := range symbols {
		byName[s.Name] = s
	}

	tests := []struct {
		name     string
		kind     string
		exported bool
		receiver string
	}{
		{"StatusCode", "type", true, ""},
		{"Handler", "interface", true, ""},
		{"DefaultTimeout", "const", true, ""},
		{"ModeRead", "const", true, ""},
		{"ModeWrite", "const", true, ""},
		{"globalLogger", "var", false, ""},
		{"Run", "func", true, ""},
		{"Result", "type", true, ""},
		{"Process", "method", true, "Result"},
		{"unexportedHelper", "func", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := byName[tt.name]
			if !ok {
				t.Fatalf("symbol %q not found", tt.name)
			}
			if s.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", s.Kind, tt.kind)
			}
			if s.Exported != tt.exported {
				t.Errorf("Exported = %v, want %v", s.Exported, tt.exported)
			}
			if s.Receiver != tt.receiver {
				t.Errorf("Receiver = %q, want %q", s.Receiver, tt.receiver)
			}
			if s.Package != "example" {
				t.Errorf("Package = %q, want %q", s.Package, "example")
			}
			if s.Line == 0 {
				t.Error("Line should be > 0")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ingest/ -run TestExtractSymbols -v
```

Expected: compilation error — `ExtractSymbols` not defined.

- [ ] **Step 3: Implement ExtractSymbols and helpers**

Add to `internal/ingest/symbol_extractor.go` (after the existing types):

```go
// ExtractSymbols parses a single Go file and returns all symbols found.
// The File field is not populated — the caller should set it.
func ExtractSymbols(filePath string) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg := file.Name.Name
	var symbols []Symbol

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			s := Symbol{
				Name:     node.Name.Name,
				Package:  pkg,
				Line:     fset.Position(node.Pos()).Line,
				Exported: token.IsExported(node.Name.Name),
			}
			if node.Doc != nil {
				s.Doc = strings.TrimSpace(node.Doc.Text())
			}
			if node.Recv != nil && len(node.Recv.List) > 0 {
				s.Kind = "method"
				s.Receiver = receiverTypeName(node.Recv.List[0].Type)
				s.Signature = fmt.Sprintf("func (%s) %s(%s)%s",
					s.Receiver, s.Name,
					formatFieldList(node.Type.Params),
					formatResults(node.Type.Results))
			} else {
				s.Kind = "func"
				s.Signature = fmt.Sprintf("func %s(%s)%s",
					s.Name,
					formatFieldList(node.Type.Params),
					formatResults(node.Type.Results))
			}
			symbols = append(symbols, s)

		case *ast.GenDecl:
			switch node.Tok {
			case token.TYPE:
				for _, spec := range node.Specs {
					ts := spec.(*ast.TypeSpec)
					s := Symbol{
						Name:     ts.Name.Name,
						Package:  pkg,
						Line:     fset.Position(ts.Pos()).Line,
						Exported: token.IsExported(ts.Name.Name),
					}
					if ts.Doc != nil {
						s.Doc = strings.TrimSpace(ts.Doc.Text())
					} else if node.Doc != nil && len(node.Specs) == 1 {
						s.Doc = strings.TrimSpace(node.Doc.Text())
					}
					if _, ok := ts.Type.(*ast.InterfaceType); ok {
						s.Kind = "interface"
						s.Signature = fmt.Sprintf("type %s interface", ts.Name.Name)
					} else {
						s.Kind = "type"
						s.Signature = fmt.Sprintf("type %s %s", ts.Name.Name, exprString(ts.Type))
					}
					symbols = append(symbols, s)
				}
			case token.CONST, token.VAR:
				kind := "const"
				if node.Tok == token.VAR {
					kind = "var"
				}
				for _, spec := range node.Specs {
					vs := spec.(*ast.ValueSpec)
					for _, name := range vs.Names {
						s := Symbol{
							Name:     name.Name,
							Kind:     kind,
							Package:  pkg,
							Line:     fset.Position(name.Pos()).Line,
							Exported: token.IsExported(name.Name),
						}
						if vs.Doc != nil {
							s.Doc = strings.TrimSpace(vs.Doc.Text())
						} else if node.Doc != nil && len(node.Specs) == 1 {
							s.Doc = strings.TrimSpace(node.Doc.Text())
						}
						if vs.Type != nil {
							s.Signature = fmt.Sprintf("%s %s %s", kind, name.Name, exprString(vs.Type))
						} else {
							s.Signature = fmt.Sprintf("%s %s", kind, name.Name)
						}
						symbols = append(symbols, s)
					}
				}
			}
		}
		return true
	})

	return symbols, nil
}

// receiverTypeName extracts the type name from a method receiver expression,
// stripping the pointer star if present.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// formatFieldList renders a field list as "name Type, name Type".
// For unnamed parameters, just "Type, Type".
func formatFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typeStr := exprString(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typeStr)
		} else {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeStr)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// formatResults renders the return type portion of a function signature.
func formatResults(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	rendered := formatFieldList(fl)
	if len(fl.List) == 1 && len(fl.List[0].Names) == 0 {
		return " " + rendered
	}
	return " (" + rendered + ")"
}

// exprString converts an ast.Expr to a readable string representation.
func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[...]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(" + formatFieldList(t.Params) + ")" + formatResults(t.Results)
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + exprString(t.Value)
		case ast.RECV:
			return "<-chan " + exprString(t.Value)
		default:
			return "chan " + exprString(t.Value)
		}
	case *ast.StructType:
		return "struct{}"
	}
	return "unknown"
}
```

Also add `"fmt"` to the import block in `symbol_extractor.go`.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestExtractSymbols -v
```

Expected: PASS. All 10 symbols found with correct Kind, Exported, Receiver, Package, and Line > 0.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "feat: implement ExtractSymbols with AST parsing"
```

---

### Task 3: Signature rendering — test and verify

**Files:**
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write TestExtractSymbols_Signature**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
func TestExtractSymbols_Signature(t *testing.T) {
	path := writeTestFile(t, testGoSource)

	symbols, err := ExtractSymbols(path)
	if err != nil {
		t.Fatalf("ExtractSymbols() error: %v", err)
	}

	byName := make(map[string]Symbol)
	for _, s := range symbols {
		byName[s.Name] = s
	}

	tests := []struct {
		name string
		want string
	}{
		{"Run", "func Run(ctx context.Context, name string) (*Result, error)"},
		{"Process", "func (Result) Process(input string) (string, bool)"},
		{"unexportedHelper", "func unexportedHelper()"},
		{"StatusCode", "type StatusCode int"},
		{"Handler", "type Handler interface"},
		{"Result", "type Result struct{}"},
		{"DefaultTimeout", "const DefaultTimeout"},
		{"ModeRead", "const ModeRead string"},
		{"globalLogger", "var globalLogger"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := byName[tt.name]
			if !ok {
				t.Fatalf("symbol %q not found", tt.name)
			}
			if s.Signature != tt.want {
				t.Errorf("Signature = %q, want %q", s.Signature, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestExtractSymbols_Signature -v
```

Expected: PASS. If any signatures don't match, fix `exprString`/`formatFieldList`/`formatResults` and re-run.

- [ ] **Step 3: Commit**

```bash
git add internal/ingest/symbol_extractor_test.go
git commit -m "test: add signature rendering tests for ExtractSymbols"
```

---

### Task 4: Doc comment extraction — test and verify

**Files:**
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write TestExtractSymbols_Doc**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
func TestExtractSymbols_Doc(t *testing.T) {
	path := writeTestFile(t, testGoSource)

	symbols, err := ExtractSymbols(path)
	if err != nil {
		t.Fatalf("ExtractSymbols() error: %v", err)
	}

	byName := make(map[string]Symbol)
	for _, s := range symbols {
		byName[s.Name] = s
	}

	tests := []struct {
		name string
		want string
	}{
		{"StatusCode", "StatusCode represents an HTTP status."},
		{"Handler", "Handler processes requests."},
		{"DefaultTimeout", "DefaultTimeout is the default timeout."},
		{"ModeRead", "ModeRead is read mode."},
		{"ModeWrite", "ModeWrite is write mode."},
		{"globalLogger", "globalLogger is the package-level logger."},
		{"Run", "Run executes the main workflow."},
		{"Process", "Process handles a single item."},
		{"unexportedHelper", ""},
		{"Result", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := byName[tt.name]
			if !ok {
				t.Fatalf("symbol %q not found", tt.name)
			}
			if s.Doc != tt.want {
				t.Errorf("Doc = %q, want %q", s.Doc, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestExtractSymbols_Doc -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ingest/symbol_extractor_test.go
git commit -m "test: add doc comment extraction tests for ExtractSymbols"
```

---

### Task 5: BuildSymbolIndex — test and implement

**Files:**
- Modify: `internal/ingest/symbol_extractor.go`
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write a test repo helper and TestBuildSymbolIndex**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
// setupSymbolTestRepo creates a temp directory tree with Go files for testing BuildSymbolIndex.
func setupSymbolTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod": "module testmod",
		"cmd/main.go": `package main

// Run starts the app.
func Run() {}
`,
		"internal/core/engine.go": `package core

import "context"

// Engine is the core engine.
type Engine struct {
	Name string
}

// Start begins engine execution.
func (e *Engine) Start(ctx context.Context) error {
	return nil
}

// NewEngine creates a new Engine.
func NewEngine(name string) *Engine {
	return &Engine{Name: name}
}
`,
		"internal/core/engine_test.go": `package core

import "testing"

func TestEngine(t *testing.T) {}
`,
		"internal/core/types.go": `package core

// Processor defines processing behavior.
type Processor interface {
	Process() error
}

// StatusActive is the active status.
const StatusActive = "active"
`,
	}

	for relPath, content := range files {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildSymbolIndex(t *testing.T) {
	root := setupSymbolTestRepo(t)

	idx, err := BuildSymbolIndex(root)
	if err != nil {
		t.Fatalf("BuildSymbolIndex() error: %v", err)
	}

	if len(idx.Symbols) == 0 {
		t.Fatal("expected symbols in index")
	}

	// Verify test files excluded by default: TestEngine should not appear.
	for _, s := range idx.Symbols {
		if s.Name == "TestEngine" {
			t.Error("test symbol TestEngine should be excluded by default")
		}
	}

	// Verify index maps are populated.
	if len(idx.ByPackage) == 0 {
		t.Error("ByPackage map is empty")
	}
	if len(idx.ByKind) == 0 {
		t.Error("ByKind map is empty")
	}
	if len(idx.ByFile) == 0 {
		t.Error("ByFile map is empty")
	}

	// Verify specific symbols exist.
	found := make(map[string]bool)
	for _, s := range idx.Symbols {
		found[s.Name] = true
	}
	for _, name := range []string{"Run", "Engine", "Start", "NewEngine", "Processor", "StatusActive"} {
		if !found[name] {
			t.Errorf("expected symbol %q in index", name)
		}
	}

	// Verify File field is set to relative paths.
	for _, s := range idx.Symbols {
		if s.File == "" {
			t.Errorf("symbol %q has empty File field", s.Name)
		}
		if filepath.IsAbs(s.File) {
			t.Errorf("symbol %q File is absolute: %s", s.Name, s.File)
		}
	}

	// Verify ByPackage grouping.
	coreSymbols := idx.ByPackage["core"]
	if len(coreSymbols) < 4 {
		t.Errorf("ByPackage[core] has %d symbols, want >= 4", len(coreSymbols))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ingest/ -run TestBuildSymbolIndex -v
```

Expected: compilation error — `BuildSymbolIndex` not defined.

- [ ] **Step 3: Implement BuildSymbolIndex**

Add to `internal/ingest/symbol_extractor.go`:

```go
// BuildSymbolIndex walks a repository and extracts symbols from all Go files.
// By default, _test.go files are excluded. Use WithTests() to include them.
func BuildSymbolIndex(repoPath string, opts ...IndexOption) (*SymbolIndex, error) {
	cfg := &indexConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var allSymbols []Symbol

	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		if !cfg.includeTests && strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}

		symbols, err := ExtractSymbols(path)
		if err != nil {
			// Skip files that fail to parse.
			return nil
		}

		for i := range symbols {
			symbols[i].File = relPath
		}
		allSymbols = append(allSymbols, symbols...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	idx := &SymbolIndex{
		Symbols:   allSymbols,
		ByPackage: make(map[string][]Symbol),
		ByKind:    make(map[string][]Symbol),
		ByFile:    make(map[string][]Symbol),
	}
	for _, s := range allSymbols {
		idx.ByPackage[s.Package] = append(idx.ByPackage[s.Package], s)
		idx.ByKind[s.Kind] = append(idx.ByKind[s.Kind], s)
		idx.ByFile[s.File] = append(idx.ByFile[s.File], s)
	}

	return idx, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestBuildSymbolIndex -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "feat: implement BuildSymbolIndex with repo walking and test exclusion"
```

---

### Task 6: BuildSymbolIndex WithTests — test and verify

**Files:**
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write TestBuildSymbolIndex_WithTests**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
func TestBuildSymbolIndex_WithTests(t *testing.T) {
	root := setupSymbolTestRepo(t)

	idx, err := BuildSymbolIndex(root, WithTests())
	if err != nil {
		t.Fatalf("BuildSymbolIndex(WithTests) error: %v", err)
	}

	found := false
	for _, s := range idx.Symbols {
		if s.Name == "TestEngine" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TestEngine symbol when WithTests() is used")
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestBuildSymbolIndex_WithTests -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ingest/symbol_extractor_test.go
git commit -m "test: add WithTests option test for BuildSymbolIndex"
```

---

### Task 7: SearchSymbols — test and implement

**Files:**
- Modify: `internal/ingest/symbol_extractor.go`
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write TestSearchSymbols**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
func TestSearchSymbols(t *testing.T) {
	root := setupSymbolTestRepo(t)

	idx, err := BuildSymbolIndex(root)
	if err != nil {
		t.Fatalf("BuildSymbolIndex() error: %v", err)
	}

	tests := []struct {
		query     string
		wantNames []string
	}{
		{"Engine", []string{"Engine", "NewEngine", "Start"}}, // Start has Engine as receiver context in signature
		{"process", []string{"Processor"}},                    // case-insensitive match on name
		{"context", []string{"Start"}},                        // matches signature containing "context.Context"
		{"active", []string{"StatusActive"}},                  // matches name
		{"nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			results := SearchSymbols(idx, tt.query)
			names := make(map[string]bool)
			for _, s := range results {
				names[s.Name] = true
			}
			for _, want := range tt.wantNames {
				if !names[want] {
					t.Errorf("expected symbol %q in results for query %q", want, tt.query)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ingest/ -run TestSearchSymbols -v
```

Expected: compilation error — `SearchSymbols` not defined.

- [ ] **Step 3: Implement SearchSymbols**

Add to `internal/ingest/symbol_extractor.go`:

```go
// SearchSymbols returns symbols whose Name, Signature, or Doc contain the query (case-insensitive).
func SearchSymbols(index *SymbolIndex, query string) []Symbol {
	q := strings.ToLower(query)
	var results []Symbol
	for _, s := range index.Symbols {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Signature), q) ||
			strings.Contains(strings.ToLower(s.Doc), q) {
			results = append(results, s)
		}
	}
	return results
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestSearchSymbols -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "feat: implement SearchSymbols with case-insensitive matching"
```

---

### Task 8: FormatSymbolContext — test and implement

**Files:**
- Modify: `internal/ingest/symbol_extractor.go`
- Modify: `internal/ingest/symbol_extractor_test.go`

- [ ] **Step 1: Write TestFormatSymbolContext**

Add to `internal/ingest/symbol_extractor_test.go`:

```go
func TestFormatSymbolContext(t *testing.T) {
	symbols := []Symbol{
		{Name: "Engine", Kind: "type", Package: "core", Signature: "type Engine struct{}", Doc: "Engine is the core engine."},
		{Name: "Start", Kind: "method", Package: "core", Signature: "func (Engine) Start(ctx context.Context) error", Doc: "Start begins engine execution."},
		{Name: "Run", Kind: "func", Package: "main", Signature: "func Run()", Doc: "Run starts the app."},
	}

	output := FormatSymbolContext(symbols)

	if output == "" {
		t.Fatal("FormatSymbolContext returned empty string")
	}

	// Verify grouping by package: "core" and "main" sections should appear.
	if !strings.Contains(output, "core") {
		t.Error("output should contain package 'core'")
	}
	if !strings.Contains(output, "main") {
		t.Error("output should contain package 'main'")
	}

	// Verify signatures appear.
	if !strings.Contains(output, "type Engine struct{}") {
		t.Error("output should contain Engine signature")
	}
	if !strings.Contains(output, "func Run()") {
		t.Error("output should contain Run signature")
	}

	// Verify docs appear.
	if !strings.Contains(output, "Engine is the core engine.") {
		t.Error("output should contain Engine doc")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ingest/ -run TestFormatSymbolContext -v
```

Expected: compilation error — `FormatSymbolContext` not defined.

- [ ] **Step 3: Implement FormatSymbolContext**

Add to `internal/ingest/symbol_extractor.go`:

```go
// FormatSymbolContext formats symbols as readable text for LLM prompts,
// grouped by package.
func FormatSymbolContext(symbols []Symbol) string {
	byPkg := make(map[string][]Symbol)
	var pkgOrder []string
	seen := make(map[string]bool)
	for _, s := range symbols {
		if !seen[s.Package] {
			seen[s.Package] = true
			pkgOrder = append(pkgOrder, s.Package)
		}
		byPkg[s.Package] = append(byPkg[s.Package], s)
	}

	var b strings.Builder
	for i, pkg := range pkgOrder {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## package %s\n\n", pkg)
		for _, s := range byPkg[pkg] {
			b.WriteString(s.Signature)
			b.WriteString("\n")
			if s.Doc != "" {
				fmt.Fprintf(&b, "  // %s\n", s.Doc)
			}
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/ingest/ -run TestFormatSymbolContext -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "feat: implement FormatSymbolContext with package grouping"
```

---

### Task 9: Run full test suite and clean up

**Files:**
- All files in `internal/ingest/`

- [ ] **Step 1: Run all tests in the ingest package**

```bash
go test ./internal/ingest/ -v
```

Expected: ALL PASS — both existing `repo_loader_test.go` tests and all new `symbol_extractor_test.go` tests.

- [ ] **Step 2: Run the full project test suite**

```bash
go test ./...
```

Expected: ALL PASS. No regressions.

- [ ] **Step 3: Run go vet**

```bash
go vet ./internal/ingest/
```

Expected: no issues.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: clean build, no errors.

- [ ] **Step 5: Final commit if any cleanup was needed, otherwise skip**

Only if steps 1-4 revealed issues that needed fixing:

```bash
git add internal/ingest/symbol_extractor.go internal/ingest/symbol_extractor_test.go
git commit -m "fix: address test/vet issues in symbol extractor"
```
