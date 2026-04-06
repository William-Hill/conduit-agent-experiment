package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestCreatePatchPlan(t *testing.T) {
	llmResponse := `{
		"plan_summary": "Update the config file to add the missing field.",
		"files_to_change": [
			{"path": "internal/config/config.go", "action": "modify", "description": "Add MaxFilesChanged field"}
		],
		"files_to_create": [
			{"path": "internal/config/defaults.go", "description": "Add default values"}
		],
		"design_choices": ["Use a constant for the default value"],
		"assumptions": ["The field is an integer"],
		"test_recommendations": ["Add unit test for default value"]
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{
		ID:          "task-001",
		Title:       "Add MaxFilesChanged config",
		Description: "Extend config to support max files changed.",
	}
	dossier := models.Dossier{
		TaskID:  "task-001",
		Summary: "Config needs updating",
		RelatedFiles: []string{"internal/config/config.go"},
	}
	fileContents := map[string]string{
		"internal/config/config.go": "package config\n\ntype Config struct{}\n",
	}

	plan, llmCall, err := CreatePatchPlan(context.Background(), client, "gemini-2.5-flash", task, dossier, fileContents)
	if err != nil {
		t.Fatalf("CreatePatchPlan() error: %v", err)
	}

	if plan.PlanSummary != "Update the config file to add the missing field." {
		t.Errorf("plan_summary = %q, want expected summary", plan.PlanSummary)
	}
	if len(plan.FilesToChange) != 1 {
		t.Errorf("files_to_change = %d, want 1", len(plan.FilesToChange))
	}
	if plan.FilesToChange[0].Path != "internal/config/config.go" {
		t.Errorf("files_to_change[0].path = %q, want %q", plan.FilesToChange[0].Path, "internal/config/config.go")
	}
	if plan.FilesToChange[0].Action != "modify" {
		t.Errorf("files_to_change[0].action = %q, want %q", plan.FilesToChange[0].Action, "modify")
	}
	if len(plan.FilesToCreate) != 1 {
		t.Errorf("files_to_create = %d, want 1", len(plan.FilesToCreate))
	}
	if len(plan.DesignChoices) != 1 {
		t.Errorf("design_choices = %d, want 1", len(plan.DesignChoices))
	}
	if len(plan.Assumptions) != 1 {
		t.Errorf("assumptions = %d, want 1", len(plan.Assumptions))
	}
	if len(plan.TestRecommendations) != 1 {
		t.Errorf("test_recommendations = %d, want 1", len(plan.TestRecommendations))
	}
	if plan.TotalFiles() != 2 {
		t.Errorf("TotalFiles() = %d, want 2", plan.TotalFiles())
	}
	if llmCall.Agent != "implementer" {
		t.Errorf("llm call agent = %q, want implementer", llmCall.Agent)
	}
}

func TestCreatePatchPlanBadJSON(t *testing.T) {
	server := mockLLMServer(t, "this is not valid json at all")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{ID: "task-001", Title: "test"}
	dossier := models.Dossier{TaskID: "task-001"}
	fileContents := map[string]string{}

	_, _, err := CreatePatchPlan(context.Background(), client, "gemini-2.5-flash", task, dossier, fileContents)
	if err == nil {
		t.Fatal("CreatePatchPlan() expected error on bad JSON, got nil")
	}
}

func TestGenerateFileContent(t *testing.T) {
	generatedCode := `package config

type Config struct {
	MaxFilesChanged int
}
`
	server := mockLLMServer(t, generatedCode)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	plan := PatchPlan{
		PlanSummary: "Add MaxFilesChanged field to Config struct.",
		FilesToChange: []FileChange{
			{Path: "internal/config/config.go", Action: "modify", Description: "Add MaxFilesChanged field"},
		},
	}
	task := models.Task{
		ID:    "task-001",
		Title: "Add MaxFilesChanged config",
	}
	currentContent := "package config\n\ntype Config struct{}\n"

	content, llmCall, err := GenerateFileContent(context.Background(), client, "gemini-2.5-flash", plan, task, "internal/config/config.go", currentContent, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("GenerateFileContent() error: %v", err)
	}
	if content != generatedCode {
		t.Errorf("content = %q, want %q", content, generatedCode)
	}
	if llmCall.Agent != "implementer" {
		t.Errorf("llm call agent = %q, want implementer", llmCall.Agent)
	}
}

func TestGenerateFileContentStripsMarkdownFences(t *testing.T) {
	wrappedCode := "```go\npackage config\n\ntype Config struct {\n\tMaxFilesChanged int\n}\n```"
	expectedCode := "package config\n\ntype Config struct {\n\tMaxFilesChanged int\n}\n"

	server := mockLLMServer(t, wrappedCode)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	plan := PatchPlan{PlanSummary: "some plan"}
	task := models.Task{ID: "task-001", Title: "test"}

	content, _, err := GenerateFileContent(context.Background(), client, "gemini-2.5-flash", plan, task, "internal/config/config.go", "", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("GenerateFileContent() error: %v", err)
	}
	if content != expectedCode {
		t.Errorf("content = %q, want %q", content, expectedCode)
	}
}

func TestBuildFileContentPromptWithSiblings(t *testing.T) {
	plan := PatchPlan{PlanSummary: "Add error constants and use them"}
	task := models.Task{ID: "test", Title: "test task", Description: "test"}
	siblings := map[string]string{
		"pkg/errors.go": "package pkg\n\nvar ErrFoo = errors.New(\"foo\")\n",
	}
	prompt := buildFileContentPrompt(plan, task, "pkg/handler.go", "package pkg", siblings, nil)
	if !strings.Contains(prompt, "Already Generated Files") {
		t.Error("prompt should contain sibling section")
	}
	if !strings.Contains(prompt, "ErrFoo") {
		t.Error("prompt should contain sibling symbol name")
	}
	if !strings.Contains(prompt, "Do not invent alternative names") {
		t.Error("prompt should contain consistency instruction")
	}
}

func TestBuildFileContentPromptNoSiblings(t *testing.T) {
	plan := PatchPlan{PlanSummary: "Simple change"}
	task := models.Task{ID: "test", Title: "test task", Description: "test"}
	prompt := buildFileContentPrompt(plan, task, "pkg/foo.go", "package pkg", nil, nil)
	if strings.Contains(prompt, "Already Generated Files") {
		t.Error("prompt should NOT contain sibling section when empty")
	}
}

func TestReviseFileContent(t *testing.T) {
	server := mockLLMServer(t, "package main\n\nfunc main() {\n\tfmt.Println(\"revised\")\n}\n")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "test-model")

	plan := PatchPlan{PlanSummary: "Fix naming"}
	task := models.Task{ID: "test", Title: "test", Description: "test"}

	content, call, err := ReviseFileContent(context.Background(), client, "test-model", plan, task, "main.go", "package main", map[string]string{}, "Please use consistent naming", nil)
	if err != nil {
		t.Fatalf("ReviseFileContent error: %v", err)
	}
	if !strings.Contains(content, "revised") {
		t.Errorf("expected revised content, got: %s", content)
	}
	if call.Agent != "implementer-revise" {
		t.Errorf("call agent = %q, want implementer-revise", call.Agent)
	}
}

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

func TestReadFileContents(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file1.go")
	file1Content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(file1, []byte(file1Content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	file2 := filepath.Join(dir, "file2.go")
	file2Content := "package config\n\ntype Config struct{}\n"
	if err := os.WriteFile(file2, []byte(file2Content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	paths := []string{"file1.go", "file2.go", "nonexistent.go"}
	result := ReadFileContents(dir, paths, 1024*1024)

	if len(result) != 2 {
		t.Errorf("ReadFileContents() returned %d entries, want 2", len(result))
	}
	if result["file1.go"] != file1Content {
		t.Errorf("file1.go content = %q, want %q", result["file1.go"], file1Content)
	}
	if result["file2.go"] != file2Content {
		t.Errorf("file2.go content = %q, want %q", result["file2.go"], file2Content)
	}
	if _, ok := result["nonexistent.go"]; ok {
		t.Error("nonexistent.go should not be present in result")
	}
}

func TestReadFileContentsTruncatesLargeFiles(t *testing.T) {
	dir := t.TempDir()

	largeContent := make([]byte, 100)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	largePath := filepath.Join(dir, "large.go")
	if err := os.WriteFile(largePath, largeContent, 0644); err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}

	result := ReadFileContents(dir, []string{"large.go"}, 10)

	content, ok := result["large.go"]
	if !ok {
		t.Fatal("large.go should be present in result")
	}
	if len(content) <= 10 {
		t.Error("truncated content should include truncation marker, making it longer than maxSize alone")
	}
	if content[len(content)-len("\n[... truncated: file exceeds size limit ...]"):] != "\n[... truncated: file exceeds size limit ...]" {
		t.Errorf("truncated content should end with truncation marker, got: %q", content)
	}
}
