package retrieval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func setupDossierRepo(t *testing.T) *ingest.FileInventory {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"docs/pipeline-config.md":                "# Pipeline Config\nYAML-based pipeline configuration.",
		"docs/design-documents/001-pipelines.md": "# ADR: Pipeline Design",
		"internal/pipeline/pipeline.go":          "package pipeline",
		"internal/pipeline/pipeline_test.go":     "package pipeline",
		"internal/pipeline/config.go":            "package pipeline\n// config handling",
		"Makefile":                               "test:\n\tgo test ./...",
		"README.md":                              "# Conduit",
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

	inv, err := ingest.WalkRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestBuildDossier(t *testing.T) {
	inv := setupDossierRepo(t)
	task := models.Task{
		ID:          "task-001",
		Title:       "Fix docs drift in pipeline config example",
		Description: "Update pipeline configuration docs to match current behavior.",
		Labels:      []string{"docs", "pipeline", "config"},
		Difficulty:  models.DifficultyL1,
		BlastRadius: models.BlastRadiusLow,
	}

	dossier := BuildDossier(task, inv)

	if dossier.TaskID != "task-001" {
		t.Errorf("TaskID = %q, want task-001", dossier.TaskID)
	}
	if dossier.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if len(dossier.RelatedFiles) == 0 {
		t.Error("expected related files")
	}
	if len(dossier.RelatedDocs) == 0 {
		t.Error("expected related docs")
	}
	if len(dossier.LikelyCommands) == 0 {
		t.Error("expected likely commands")
	}

	// Pipeline config doc should be in related files or docs.
	foundPipelineConfig := false
	for _, f := range dossier.RelatedFiles {
		if f == "docs/pipeline-config.md" {
			foundPipelineConfig = true
			break
		}
	}
	// It might be classified as docs instead of files
	if !foundPipelineConfig {
		for _, d := range dossier.RelatedDocs {
			if d == "docs/pipeline-config.md" {
				foundPipelineConfig = true
				break
			}
		}
	}
	if !foundPipelineConfig {
		t.Errorf("expected docs/pipeline-config.md in related files or docs, got files=%v docs=%v", dossier.RelatedFiles, dossier.RelatedDocs)
	}

	// ADR should appear in related docs.
	foundADR := false
	for _, d := range dossier.RelatedDocs {
		if d == "docs/design-documents/001-pipelines.md" {
			foundADR = true
			break
		}
	}
	if !foundADR {
		t.Errorf("expected ADR in related docs, got %v", dossier.RelatedDocs)
	}
}
