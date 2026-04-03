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
