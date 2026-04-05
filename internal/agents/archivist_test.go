package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func mockLLMServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gemini-2.5-flash",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": responseContent,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 50,
				"total_tokens":      60,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEnhanceDossier(t *testing.T) {
	llmResponse := `{
		"summary": "Update pipeline config docs to match current YAML syntax",
		"relevant_files": ["docs/pipeline-config.md", "internal/pipeline/config.go"],
		"relevant_docs": ["docs/design-documents/001-pipelines.md"],
		"suggested_commands": ["make test", "go build ./..."],
		"risks": ["Config examples may be referenced in external docs"],
		"open_questions": ["Are there other config files affected?"]
	}`

	server := mockLLMServer(t, llmResponse)
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config",
		Description: "Update pipeline configuration docs.",
		Labels:      []string{"docs", "pipeline"},
	}
	original := models.Dossier{
		TaskID:         "task-001",
		Summary:        "keyword-based summary",
		RelatedFiles:   []string{"file1.go", "file2.go", "file3.go"},
		RelatedDocs:    []string{"doc1.md"},
		LikelyCommands: []string{"go test ./..."},
		Risks:          []string{"none"},
		OpenQuestions:  []string{"original question"},
	}

	enhanced, llmCalls, err := EnhanceDossier(context.Background(), client, "gemini-2.5-flash", task, original)
	if err != nil {
		t.Fatalf("EnhanceDossier() error: %v", err)
	}
	if enhanced.Summary != "Update pipeline config docs to match current YAML syntax" {
		t.Errorf("summary = %q, want LLM-enhanced summary", enhanced.Summary)
	}
	if len(enhanced.RelatedFiles) != 2 {
		t.Errorf("related files = %d, want 2", len(enhanced.RelatedFiles))
	}
	if len(llmCalls) != 1 {
		t.Errorf("llm calls = %d, want 1 (no retry on success)", len(llmCalls))
	}
	if len(llmCalls) > 0 && llmCalls[0].Agent != "archivist" {
		t.Errorf("llm call agent = %q, want archivist", llmCalls[0].Agent)
	}
}

func TestEnhanceDossierFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"fail"}}`))
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{ID: "task-001", Title: "test"}
	original := models.Dossier{
		TaskID:  "task-001",
		Summary: "original summary",
	}

	enhanced, _, err := EnhanceDossier(context.Background(), client, "gemini-2.5-flash", task, original)
	if err != nil {
		t.Fatalf("expected fallback, not error: %v", err)
	}
	if enhanced.Summary != "original summary" {
		t.Errorf("should fall back to original dossier, got summary=%q", enhanced.Summary)
	}
}

func TestEnhanceDossierBadJSON(t *testing.T) {
	server := mockLLMServer(t, "this is not valid json at all")
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", "gemini-2.5-flash")

	task := models.Task{ID: "task-001", Title: "test"}
	original := models.Dossier{
		TaskID:  "task-001",
		Summary: "original summary",
	}

	enhanced, _, err := EnhanceDossier(context.Background(), client, "gemini-2.5-flash", task, original)
	if err != nil {
		t.Fatalf("expected fallback on bad JSON, not error: %v", err)
	}
	if enhanced.Summary != "original summary" {
		t.Error("should fall back to original dossier on bad JSON")
	}
}
