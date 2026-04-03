package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

// helper creates a temp directory tree mimicking a Go repo.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"README.md":                           "# Project",
		"go.mod":                              "module example",
		"Makefile":                            "build:",
		".github/workflows/ci.yml":            "on: push",
		"cmd/server/main.go":                  "package main",
		"internal/core/engine.go":             "package core",
		"internal/core/engine_test.go":        "package core",
		"docs/design-documents/001-init.md":   "# ADR 001",
		"docs/architecture.md":                "# Architecture",
		"config.yaml":                         "key: value",
		"pkg/plugin/connector.go":             "package plugin",
		"pkg/plugin/connector_test.go":        "package plugin",
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
	return root
}

func TestWalkRepo(t *testing.T) {
	root := setupTestRepo(t)

	inv, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo() error: %v", err)
	}

	if len(inv.Files) == 0 {
		t.Fatal("expected files in inventory")
	}

	// Check that classification works for known files.
	counts := make(map[FileCategory]int)
	for _, f := range inv.Files {
		counts[f.Category]++
	}

	if counts[CategoryTest] != 2 {
		t.Errorf("test files = %d, want 2", counts[CategoryTest])
	}
	if counts[CategoryWorkflow] != 1 {
		t.Errorf("workflow files = %d, want 1", counts[CategoryWorkflow])
	}
	if counts[CategoryADR] < 1 {
		t.Errorf("ADR files = %d, want >= 1", counts[CategoryADR])
	}
	if counts[CategoryDocs] < 1 {
		t.Errorf("doc files = %d, want >= 1", counts[CategoryDocs])
	}
	if counts[CategoryConfig] < 1 {
		t.Errorf("config files = %d, want >= 1", counts[CategoryConfig])
	}
	if counts[CategoryCode] < 3 {
		t.Errorf("code files = %d, want >= 3", counts[CategoryCode])
	}
}

func TestClassifyFile(t *testing.T) {
	tests := []struct {
		path string
		want FileCategory
	}{
		{"internal/core/engine_test.go", CategoryTest},
		{"internal/core/engine.go", CategoryCode},
		{".github/workflows/ci.yml", CategoryWorkflow},
		{"docs/design-documents/001-init.md", CategoryADR},
		{"docs/architecture.md", CategoryDocs},
		{"README.md", CategoryDocs},
		{"config.yaml", CategoryConfig},
		{"Makefile", CategoryConfig},
		{"go.mod", CategoryConfig},
		{"cmd/server/main.go", CategoryCode},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ClassifyFile(tt.path)
			if got != tt.want {
				t.Errorf("ClassifyFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFilesByCategory(t *testing.T) {
	root := setupTestRepo(t)
	inv, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo() error: %v", err)
	}

	tests := inv.FilesByCategory(CategoryTest)
	if len(tests) != 2 {
		t.Errorf("FilesByCategory(test) = %d files, want 2", len(tests))
	}
}
