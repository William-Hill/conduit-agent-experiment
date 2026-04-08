package implementer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// findTool returns the tool with the given name, or nil if not found.
func findTool(tools []anthropic.BetaTool, name string) anthropic.BetaTool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

// execTool executes a tool with the given JSON input and returns the text result.
func execTool(t *testing.T, tool anthropic.BetaTool, inputJSON string) string {
	t.Helper()
	result, err := tool.Execute(context.Background(), json.RawMessage(inputJSON))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.OfText == nil {
		t.Fatal("Expected text result, got nil OfText")
	}
	return result.OfText.Text
}

func TestNewToolsCount(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatalf("NewTools: %v", err)
	}
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}

	expected := []string{"read_file", "write_file", "list_dir", "search_files", "run_command"}
	for _, name := range expected {
		if findTool(tools, name) == nil {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "read_file")
	if tool == nil {
		t.Fatal("read_file tool not found")
	}

	result := execTool(t, tool, `{"path":"main.go"}`)
	if result != content {
		t.Errorf("read_file returned %q, want %q", result, content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "read_file")
	if tool == nil {
		t.Fatal("read_file tool not found")
	}

	result := execTool(t, tool, `{"path":"nonexistent.go"}`)
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error message for nonexistent file, got: %q", result)
	}
}

func TestReadFilePathTraversal(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "read_file")
	if tool == nil {
		t.Fatal("read_file tool not found")
	}

	result := execTool(t, tool, `{"path":"../../etc/passwd"}`)
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error for path traversal, got: %q", result)
	}
	if !strings.Contains(result, "escapes") {
		t.Errorf("expected 'escapes' in error message, got: %q", result)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "write_file")
	if tool == nil {
		t.Fatal("write_file tool not found")
	}

	content := "package sub\n\nfunc Hello() {}\n"
	result := execTool(t, tool, `{"path":"sub/new.go","content":"`+strings.ReplaceAll(content, "\n", "\\n")+`"}`)
	if !strings.Contains(result, "sub/new.go") {
		t.Errorf("expected confirmation with path, got: %q", result)
	}

	// Verify file on disk
	data, err := os.ReadFile(filepath.Join(dir, "sub", "new.go"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

func TestReadFileSiblingPrefixEscape(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "read_file")

	// A path like "../<dirname>-escape/file" resolves to a sibling that shares
	// the same prefix — this should still be blocked.
	result := execTool(t, tool, `{"path":"../`+filepath.Base(dir)+`-escape/secret.txt"}`)
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error for sibling-prefix escape, got: %q", result)
	}
}

func TestReadFileSymlinkEscape(t *testing.T) {
	dir := t.TempDir()

	// Create a file outside the repo dir
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"), []byte("sensitive data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside dir that points outside
	if err := os.Symlink(outsideDir, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "read_file")

	result := execTool(t, tool, `{"path":"escape/secret.txt"}`)
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error for symlink escape, got: %q", result)
	}
	if !strings.Contains(result, "escapes") {
		t.Errorf("expected escape-specific error, got: %q", result)
	}
}

func TestWriteFilePathTraversal(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "write_file")
	if tool == nil {
		t.Fatal("write_file tool not found")
	}

	result := execTool(t, tool, `{"path":"../../tmp/evil.txt","content":"bad"}`)
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error for path traversal, got: %q", result)
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	// Create files and a subdirectory
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644)
	os.Mkdir(filepath.Join(dir, "internal"), 0o755)

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "list_dir")
	if tool == nil {
		t.Fatal("list_dir tool not found")
	}

	result := execTool(t, tool, `{"path":"."}`)
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in listing, got: %q", result)
	}
	if !strings.Contains(result, "go.mod") {
		t.Errorf("expected go.mod in listing, got: %q", result)
	}
	if !strings.Contains(result, "internal/") {
		t.Errorf("expected internal/ (with slash) in listing, got: %q", result)
	}
}

func TestListDirEmpty(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "list_dir")

	// Empty path should list root
	result := execTool(t, tool, `{}`)
	// Should not error (empty dir is fine)
	if strings.Contains(result, "Error:") {
		t.Errorf("expected no error for empty dir, got: %q", result)
	}
}

func TestSearchFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package main\n\nfunc helper() {}\n"), 0o644)

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "search_files")
	if tool == nil {
		t.Fatal("search_files tool not found")
	}

	result := execTool(t, tool, `{"pattern":"func main"}`)
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in search results, got: %q", result)
	}

	// Search with glob filter
	result = execTool(t, tool, `{"pattern":"func","glob":"*.go"}`)
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected main.go in glob search results, got: %q", result)
	}
	if !strings.Contains(result, "lib.go") {
		t.Errorf("expected lib.go in glob search results, got: %q", result)
	}
}

func TestSearchFilesNoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "search_files")
	if tool == nil {
		t.Fatal("search_files tool not found")
	}

	result := execTool(t, tool, `{"pattern":"nonexistent_xyz"}`)
	if strings.Contains(result, "Error:") {
		t.Errorf("no match should not be an error, got: %q", result)
	}
}

func TestRunCommand(t *testing.T) {
	dir := t.TempDir()
	// Initialize a go module so `go vet` can run
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "run_command")
	if tool == nil {
		t.Fatal("run_command tool not found")
	}

	result := execTool(t, tool, `{"command":"go vet ./..."}`)
	if !strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected exit_code 0, got: %q", result)
	}
}

func TestRunCommandDisallowed(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "run_command")

	result := execTool(t, tool, `{"command":"rm -rf /"}`)
	if !strings.Contains(result, "not allowed") {
		t.Errorf("expected 'not allowed' error, got: %q", result)
	}
}

func TestRunCommandPathQualified(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "run_command")

	result := execTool(t, tool, `{"command":"./go test"}`)
	if !strings.Contains(result, "bare name") {
		t.Errorf("expected 'bare name' error for path-qualified command, got: %q", result)
	}
}

func TestRunCommandDisallowedSubcommand(t *testing.T) {
	dir := t.TempDir()
	tools, err := NewTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool := findTool(tools, "run_command")

	result := execTool(t, tool, `{"command":"git push --force"}`)
	if !strings.Contains(result, "not allowed") {
		t.Errorf("expected 'not allowed' error for git push, got: %q", result)
	}
}
