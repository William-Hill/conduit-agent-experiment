package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/reporting"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/retrieval"
)

func TestEndToEnd(t *testing.T) {
	// Set up a fake target repo.
	repoDir := t.TempDir()
	repoFiles := map[string]string{
		"README.md":                              "# Conduit\nA streaming data platform.",
		"docs/pipeline-config.md":                "# Pipeline Config\nYAML-based pipeline configuration.",
		"docs/design-documents/001-pipelines.md": "# ADR: Pipeline Design",
		"internal/pipeline/pipeline.go":          "package pipeline",
		"Makefile":                               "test:\n\tgo test ./...",
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

	// Set up config.
	cfgDir := t.TempDir()
	cfgContent := []byte(`target:
  repo_path: "` + repoDir + `"
  ref: "main"
reporting:
  output_dir: "` + filepath.Join(cfgDir, "runs") + `"
  formats:
    - json
    - markdown
`)
	cfgPath := filepath.Join(cfgDir, "experiment.yaml")
	if err := os.WriteFile(cfgPath, cfgContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Set up task.
	task := models.Task{
		ID:                 "task-001",
		Title:              "Fix docs drift in pipeline config example",
		Source:             "seeded",
		Description:        "Update pipeline configuration docs to match current behavior.",
		Labels:             []string{"docs", "pipeline", "config"},
		Difficulty:         models.DifficultyL1,
		BlastRadius:        models.BlastRadiusLow,
		AcceptanceCriteria: []string{"Docs updated"},
		Status:             models.TaskStatusPending,
	}

	// Load config.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load() error: %v", err)
	}

	// Policy check.
	policy := orchestrator.DefaultPhase1Policy()
	if err := policy.CheckTask(task); err != nil {
		t.Fatalf("policy check failed: %v", err)
	}

	// Ingest.
	inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
	if err != nil {
		t.Fatalf("WalkRepo() error: %v", err)
	}
	if len(inv.Files) == 0 {
		t.Fatal("expected files in inventory")
	}

	// Build dossier.
	dossier := retrieval.BuildDossier(task, inv)
	if dossier.TaskID != "task-001" {
		t.Errorf("dossier task ID = %q, want task-001", dossier.TaskID)
	}
	if len(dossier.RelatedFiles) == 0 && len(dossier.RelatedDocs) == 0 {
		t.Error("dossier has no related files or docs")
	}

	// Create run.
	run := models.Run{
		ID:            "run-test-001",
		TaskID:        task.ID,
		AgentsInvoked: []string{"archivist"},
		FinalStatus:   models.RunStatusSuccess,
		HumanDecision: models.HumanDecisionPending,
	}

	// Write outputs.
	outDir := filepath.Join(cfg.Reporting.OutputDir, run.ID)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := reporting.WriteRunJSON(outDir, run); err != nil {
		t.Fatalf("WriteRunJSON() error: %v", err)
	}
	if err := reporting.WriteDossierJSON(outDir, dossier); err != nil {
		t.Fatalf("WriteDossierJSON() error: %v", err)
	}

	md, err := reporting.RenderMarkdown(run, dossier, task)
	if err != nil {
		t.Fatalf("RenderMarkdown() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify outputs exist.
	for _, name := range []string{"run.json", "dossier.json", "report.md"} {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// Verify run.json is valid.
	runData, _ := os.ReadFile(filepath.Join(outDir, "run.json"))
	var loadedRun models.Run
	if err := json.Unmarshal(runData, &loadedRun); err != nil {
		t.Fatalf("run.json invalid: %v", err)
	}
	if loadedRun.ID != "run-test-001" {
		t.Errorf("loaded run ID = %q, want run-test-001", loadedRun.ID)
	}

	// Verify dossier.json is valid.
	dossierData, _ := os.ReadFile(filepath.Join(outDir, "dossier.json"))
	var loadedDossier models.Dossier
	if err := json.Unmarshal(dossierData, &loadedDossier); err != nil {
		t.Fatalf("dossier.json invalid: %v", err)
	}
	if loadedDossier.TaskID != "task-001" {
		t.Errorf("loaded dossier task ID = %q, want task-001", loadedDossier.TaskID)
	}
}
