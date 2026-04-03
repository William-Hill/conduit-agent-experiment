package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestRunWorkflowAccepted(t *testing.T) {
	repoDir := t.TempDir()
	files := map[string]string{
		"README.md":                         "# Test Project",
		"Makefile":                          "test:\n\techo ok",
		"docs/design-documents/001-init.md": "# ADR 001",
		"internal/pipeline/pipeline.go":     "package pipeline",
	}
	for relPath, content := range files {
		full := filepath.Join(repoDir, relPath)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	llmResp := `{"summary":"Enhanced summary","relevant_files":["README.md"],"relevant_docs":["docs/design-documents/001-init.md"],"suggested_commands":["echo test"],"risks":["none"],"open_questions":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id": "test", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": llmResp},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Execution: config.ExecutionConfig{UseWorktree: false, TimeoutSeconds: 10},
		Reporting: config.ReportingConfig{OutputDir: t.TempDir()},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift",
		Description: "Update docs.",
		Labels:      []string{"docs"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
		Status:      models.TaskStatusPending,
	}

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != "accept" {
		t.Errorf("triage = %q, want accept", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusSuccess {
		t.Errorf("status = %q, want success", result.Run.FinalStatus)
	}
	if len(result.LLMCalls) == 0 {
		t.Error("expected at least one LLM call")
	}
}

func TestRunWorkflowRejected(t *testing.T) {
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644)

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir},
		Execution: config.ExecutionConfig{TimeoutSeconds: 10},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: "http://unused"},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-002",
		Title:       "Dangerous task",
		Difficulty:  models.DifficultyL4,
		BlastRadius: models.BlastRadiusHigh,
	}

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != "reject" {
		t.Errorf("triage = %q, want reject", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusRejected {
		t.Errorf("status = %q, want rejected", result.Run.FinalStatus)
	}
}
