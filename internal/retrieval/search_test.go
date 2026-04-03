package retrieval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

func setupSearchRepo(t *testing.T) *ingest.FileInventory {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"docs/pipeline-config.md":        "# Pipeline Configuration\nThe pipeline uses YAML config files.",
		"docs/architecture.md":           "# Architecture\nCore runtime manages connectors.",
		"internal/pipeline/pipeline.go":  "package pipeline\n// Pipeline runs connectors.",
		"internal/pipeline/config.go":    "package pipeline\n// Config holds pipeline config.",
		"internal/connector/source.go":   "package connector\n// Source reads data.",
		"README.md":                      "# Conduit\nA streaming data platform.",
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

func TestSearchByKeyword(t *testing.T) {
	inv := setupSearchRepo(t)

	results := SearchByKeyword(inv, []string{"pipeline", "config"})
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	// Files with "pipeline" or "config" in their path should appear.
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Path] = true
	}

	wantPaths := []string{
		"docs/pipeline-config.md",
		"internal/pipeline/pipeline.go",
		"internal/pipeline/config.go",
	}
	for _, p := range wantPaths {
		if !found[p] {
			t.Errorf("expected %q in results, got %v", p, results)
		}
	}
}

func TestSearchByContent(t *testing.T) {
	inv := setupSearchRepo(t)

	results := SearchByContent(inv, []string{"connectors"})
	if len(results) == 0 {
		t.Fatal("expected content search results")
	}

	// architecture.md and pipeline.go mention "connectors".
	found := make(map[string]bool)
	for _, r := range results {
		found[r.Path] = true
	}
	if !found["docs/architecture.md"] {
		t.Error("expected docs/architecture.md in content results")
	}
	if !found["internal/pipeline/pipeline.go"] {
		t.Error("expected internal/pipeline/pipeline.go in content results")
	}
}
