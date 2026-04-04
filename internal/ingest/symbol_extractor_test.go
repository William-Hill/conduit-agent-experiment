package ingest

import (
	"os"
	"path/filepath"
	"strings"
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
