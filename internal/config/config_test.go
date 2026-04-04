package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "experiment.yaml")
	content := []byte(`target:
  repo_path: "/tmp/conduit"
  ref: "main"
policy:
  max_difficulty: "L2"
  max_blast_radius: "medium"
  allow_push: false
  allow_merge: false
  require_rationale: true
execution:
  use_worktree: true
  timeout_seconds: 300
reporting:
  output_dir: "data/runs"
  formats:
    - json
    - markdown
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Target.RepoPath != "/tmp/conduit" {
		t.Errorf("RepoPath = %q, want /tmp/conduit", cfg.Target.RepoPath)
	}
	if cfg.Target.Ref != "main" {
		t.Errorf("Ref = %q, want main", cfg.Target.Ref)
	}
	if cfg.Reporting.OutputDir != "data/runs" {
		t.Errorf("OutputDir = %q, want data/runs", cfg.Reporting.OutputDir)
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "experiment.yaml")
	content := []byte(`target:
  repo_path: "/tmp/conduit"
  ref: "main"
reporting:
  output_dir: "data/runs"
  formats:
    - json
    - markdown
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONDUIT_REPO_PATH", "/override/path")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Target.RepoPath != "/override/path" {
		t.Errorf("RepoPath = %q, want /override/path", cfg.Target.RepoPath)
	}
}

func TestLoadModels(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.yaml")
	content := []byte(`provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"

roles:
  archivist:
    model: "gemini-2.5-flash"
  triage:
    model: "gemini-2.5-flash"
`)
	if err := os.WriteFile(modelsPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	mcfg, err := LoadModels(modelsPath)
	if err != nil {
		t.Fatalf("LoadModels() error: %v", err)
	}
	if mcfg.Provider.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai/" {
		t.Errorf("BaseURL = %q, want Gemini endpoint", mcfg.Provider.BaseURL)
	}
	if mcfg.Roles["archivist"].Model != "gemini-2.5-flash" {
		t.Errorf("archivist model = %q, want gemini-2.5-flash", mcfg.Roles["archivist"].Model)
	}
}

func TestLoadModelsAPIKeyFromEnv(t *testing.T) {
	dir := t.TempDir()
	modelsPath := filepath.Join(dir, "models.yaml")
	content := []byte(`provider:
  base_url: "https://generativelanguage.googleapis.com/v1beta/openai/"
roles:
  archivist:
    model: "gemini-2.5-flash"
`)
	if err := os.WriteFile(modelsPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GEMINI_API_KEY", "test-key-123")

	mcfg, err := LoadModels(modelsPath)
	if err != nil {
		t.Fatalf("LoadModels() error: %v", err)
	}
	if mcfg.APIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want test-key-123", mcfg.APIKey)
	}
}
