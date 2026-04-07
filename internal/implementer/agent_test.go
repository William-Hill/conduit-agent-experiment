package implementer

import (
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
)

func TestExtractTextNil(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Errorf("extractText(nil) = %q, want empty", got)
	}
}

func TestBuildPromptWithDossier(t *testing.T) {
	d := &archivist.Dossier{
		Summary:  "The handler needs error wrapping",
		Approach: "Add fmt.Errorf with %w",
		Files: []archivist.FileEntry{
			{Path: "pkg/api.go", Reason: "Contains the handler", Content: "package api\n"},
		},
		Risks: []string{"May affect clients"},
	}
	prompt := buildPrompt("Fix error handling", "Errors are returned as 500", d)
	if !strings.Contains(prompt, "Archivist Research") {
		t.Error("prompt missing archivist section")
	}
	if !strings.Contains(prompt, "package api") {
		t.Error("prompt missing file content")
	}
	if !strings.Contains(prompt, "May affect clients") {
		t.Error("prompt missing risks")
	}
}

func TestBuildPromptNilDossier(t *testing.T) {
	prompt := buildPrompt("Fix bug", "Something is broken", nil)
	if strings.Contains(prompt, "Archivist") {
		t.Error("nil dossier should not add archivist section")
	}
	if !strings.Contains(prompt, "Fix bug") {
		t.Error("prompt missing issue title")
	}
}
