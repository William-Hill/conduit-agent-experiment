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
