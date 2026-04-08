package archivist

import (
	"path/filepath"
	"testing"
)

func TestSaveThenLoadDossier(t *testing.T) {
	dir := t.TempDir()
	want := Dossier{
		Summary: "Fix error handling in API layer",
		Files: []FileEntry{
			{Path: "pkg/api/handler.go", Reason: "Contains the broken handler", Content: "package api\n"},
		},
		Risks:    []string{"May affect downstream clients"},
		Approach: "Add proper error wrapping in the handler",
	}

	path, err := SaveDossier(dir, want)
	if err != nil {
		t.Fatalf("SaveDossier: %v", err)
	}
	if filepath.Base(path) != "dossier.json" {
		t.Errorf("filename = %q, want dossier.json", filepath.Base(path))
	}

	got, err := LoadDossier(path)
	if err != nil {
		t.Fatalf("LoadDossier: %v", err)
	}
	if got.Summary != want.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, want.Summary)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "pkg/api/handler.go" {
		t.Errorf("Files mismatch")
	}
	if got.Files[0].Content != "package api\n" {
		t.Errorf("Content = %q, want %q", got.Files[0].Content, "package api\n")
	}
	if got.Approach != want.Approach {
		t.Errorf("Approach = %q, want %q", got.Approach, want.Approach)
	}
	if len(got.Risks) != 1 || got.Risks[0] != want.Risks[0] {
		t.Errorf("Risks = %v, want %v", got.Risks, want.Risks)
	}
	if got.Files[0].Reason != want.Files[0].Reason {
		t.Errorf("Reason = %q, want %q", got.Files[0].Reason, want.Files[0].Reason)
	}
}
