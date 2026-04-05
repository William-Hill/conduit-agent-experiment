package main

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
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/reporting"
)

func TestEndToEnd(t *testing.T) {
	repoDir := t.TempDir()
	repoFiles := map[string]string{
		"README.md":                              "# Conduit\nA streaming data platform.",
		"docs/pipeline-config.md":                "# Pipeline Config\nYAML-based pipeline configuration.",
		"docs/design-documents/001-pipelines.md": "# ADR: Pipeline Design",
		"internal/pipeline/pipeline.go":          "package pipeline",
		"Makefile":                               "test:\n\techo ok",
	}
	for relPath, content := range repoFiles {
		full := filepath.Join(repoDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	llmResp := `{"summary":"Enhanced task summary","relevant_files":["README.md"],"relevant_docs":["docs/design-documents/001-pipelines.md"],"suggested_commands":["echo test"],"risks":["none"],"open_questions":[]}`
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

	outDir := t.TempDir()
	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Execution: config.ExecutionConfig{UseWorktree: false, TimeoutSeconds: 10},
		Reporting: config.ReportingConfig{OutputDir: outDir},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		Roles:    map[string]config.RoleConfig{"archivist": {Model: "test"}},
		APIKey:   "test-key",
	}

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config example",
		Source:      "seeded",
		Description: "Update pipeline configuration docs.",
		Labels:      []string{"docs", "pipeline"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
		Status:      models.TaskStatusPending,
	}

	result, err := orchestrator.RunWorkflow(context.Background(), task, cfg, mcfg, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}

	if result.TriageDecision.Decision != "accept" {
		t.Fatalf("triage = %q, want accept", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusSuccess {
		t.Errorf("status = %q, want success", result.Run.FinalStatus)
	}

	runDir := filepath.Join(outDir, result.Run.ID)
	os.MkdirAll(runDir, 0755)

	if err := reporting.WriteRunJSON(runDir, result.Run); err != nil {
		t.Fatalf("WriteRunJSON error: %v", err)
	}
	if err := reporting.WriteDossierJSON(runDir, result.Dossier); err != nil {
		t.Fatalf("WriteDossierJSON error: %v", err)
	}
	md, err := reporting.RenderMarkdown(result.Run, result.Dossier, result.Task)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	os.WriteFile(filepath.Join(runDir, "report.md"), []byte(md), 0644)

	for _, name := range []string{"run.json", "dossier.json", "report.md"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	runData, _ := os.ReadFile(filepath.Join(runDir, "run.json"))
	var loadedRun models.Run
	if err := json.Unmarshal(runData, &loadedRun); err != nil {
		t.Fatalf("run.json invalid: %v", err)
	}
	if loadedRun.TriageDecision != "accept" {
		t.Errorf("run.json triage = %q, want accept", loadedRun.TriageDecision)
	}
}
