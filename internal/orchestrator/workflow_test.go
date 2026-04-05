package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
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

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != agents.DecisionAccept {
		t.Errorf("triage = %q, want accept", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusSuccess && result.Run.FinalStatus != models.RunStatusFailed {
		t.Errorf("status = %q, want success or failed", result.Run.FinalStatus)
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

	result, err := RunWorkflow(context.Background(), task, cfg, mcfg, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}
	if result.TriageDecision.Decision != agents.DecisionReject {
		t.Errorf("triage = %q, want reject", result.TriageDecision.Decision)
	}
	if result.Run.FinalStatus != models.RunStatusRejected {
		t.Errorf("status = %q, want rejected", result.Run.FinalStatus)
	}
}

func newMultiResponseLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		systemPrompt := ""
		if len(req.Messages) > 0 {
			systemPrompt = req.Messages[0].Content
		}

		// Match LLM responses to agent roles based on system prompt keywords.
		// Update these if agent system prompts change:
		//   "archivist"              -> Archivist dossier enhancement
		//   "implementing a patch"   -> Implementer Phase 1 (patch plan)
		//   "Generate the complete"  -> Implementer Phase 2 (code generation)
		//   "architect"              -> Architect review
		var content string
		switch {
		case strings.Contains(systemPrompt, "archivist"):
			content = `{"summary":"Enhanced summary for test","relevant_files":["main.go"],"relevant_docs":["docs/adr/001-init.md"],"suggested_commands":["go build ./..."],"risks":["none"],"open_questions":[]}`
		case strings.Contains(systemPrompt, "implementing a patch"):
			// Implementer plan (CreatePatchPlan)
			content = `{"plan_summary":"Update main.go to add a greeting function","files_to_change":[{"path":"main.go","action":"modify","description":"Add greeting function"}],"files_to_create":[],"design_choices":["Keep it simple"],"assumptions":["Single file change"],"test_recommendations":["go build ./..."]}`
		case strings.Contains(systemPrompt, "Generate the complete"):
			// Implementer file content (GenerateFileContent)
			content = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n\nfunc greet(name string) string {\n\treturn \"Hello, \" + name\n}\n"
		case strings.Contains(systemPrompt, "architect"):
			content = `{"recommendation":"approve","confidence":"high","alignment_notes":"Change is well-scoped","risks_identified":[],"adr_conflicts":[],"suggestions":[],"rationale":"The change is minimal, well-tested, and aligned with project architecture."}`
		default:
			content = `{"summary":"generic response","relevant_files":[],"relevant_docs":[],"suggested_commands":[],"risks":[],"open_questions":[]}`
		}

		resp := map[string]any{
			"id": "test", "object": "chat.completion", "created": 0, "model": "test",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name Test",
		"git add -A",
		"git commit -m 'initial commit'",
	}
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git init step %q failed: %v\n%s", c, err, out)
		}
	}
}

func TestRunWorkflowM2(t *testing.T) {
	// Create a temp git repo with a Go file and an ADR doc.
	repoDir := t.TempDir()

	files := map[string]string{
		"main.go":             "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n",
		"docs/adr/001-init.md": "# ADR 001: Initial Architecture\n\nKeep things simple.\n",
		"go.mod":               "module example.com/test\n\ngo 1.21\n",
	}
	for relPath, content := range files {
		full := filepath.Join(repoDir, relPath)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	initGitRepo(t, repoDir)

	server := newMultiResponseLLMServer(t)
	defer server.Close()

	cfg := config.Config{
		Target:    config.TargetConfig{RepoPath: repoDir, Ref: "main"},
		Execution: config.ExecutionConfig{UseWorktree: false, TimeoutSeconds: 30},
		Reporting: config.ReportingConfig{OutputDir: t.TempDir()},
	}
	mcfg := config.ModelsConfig{
		Provider: config.ProviderConfig{BaseURL: server.URL},
		Roles: map[string]config.RoleConfig{
			"archivist":   {Model: "test"},
			"implementer": {Model: "test"},
			"architect":   {Model: "test"},
		},
		APIKey: "test-key",
	}

	task := models.Task{
		ID:          "task-m2-001",
		Title:       "Add greeting function",
		Description: "Add a greet function to main.go.",
		Labels:      []string{"enhancement"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
		Status:      models.TaskStatusPending,
	}

	// Run with nil adapter = skip PR creation.
	result, err := RunWorkflow(context.Background(), task, cfg, mcfg, nil)
	if err != nil {
		t.Fatalf("RunWorkflow() error: %v", err)
	}

	// Verify: PatchPlan.PlanSummary not empty.
	if result.PatchPlan.PlanSummary == "" {
		t.Error("expected PatchPlan.PlanSummary to be non-empty")
	}

	// Verify: ArchitectReview.Recommendation is "approve".
	if result.ArchitectReview.Recommendation != agents.RecommendApprove {
		t.Errorf("ArchitectReview.Recommendation = %q, want %q", result.ArchitectReview.Recommendation, agents.RecommendApprove)
	}

	// Verify: LLMCalls >= 3 (archivist + implementer plan + implementer code gen + architect).
	if len(result.LLMCalls) < 3 {
		t.Errorf("len(LLMCalls) = %d, want >= 3", len(result.LLMCalls))
	}

	// Verify: Run.ImplementerDiff not empty.
	if result.Run.ImplementerDiff == "" {
		t.Error("expected Run.ImplementerDiff to be non-empty")
	}

	// Verify: Run.ImplementerPlan is set.
	if result.Run.ImplementerPlan == "" {
		t.Error("expected Run.ImplementerPlan to be non-empty")
	}

	// Verify: Run.ArchitectDecision is set.
	if result.Run.ArchitectDecision != agents.RecommendApprove {
		t.Errorf("Run.ArchitectDecision = %q, want %q", result.Run.ArchitectDecision, agents.RecommendApprove)
	}

	// Verify: PRURL is empty since we passed nil adapter.
	if result.PRURL != "" {
		t.Errorf("PRURL = %q, want empty (nil adapter)", result.PRURL)
	}

	// Verify: Evaluation was built.
	if result.Evaluation.RunID == "" {
		t.Error("expected Evaluation.RunID to be non-empty")
	}

	// Verify: Final status.
	if result.Run.FinalStatus != models.RunStatusSuccess {
		t.Errorf("FinalStatus = %q, want %q", result.Run.FinalStatus, models.RunStatusSuccess)
	}
}
