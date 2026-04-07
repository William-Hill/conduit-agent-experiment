package implementer

import (
	"strings"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

func TestExtractTextNil(t *testing.T) {
	if got := extractText(nil); got != "" {
		t.Errorf("extractText(nil) = %q, want empty", got)
	}
}

func TestBuildPromptWithPlan(t *testing.T) {
	p := &planner.ImplementationPlan{
		Summary: "Add error codes to Swagger",
		Changes: []planner.PlannedChange{
			{Path: "pkg/api.go", Description: "Update error handling", Content: "package api\n"},
		},
		Verification: []string{"go build ./..."},
	}
	prompt := buildPrompt(p)
	if !strings.Contains(prompt, "pkg/api.go") {
		t.Error("missing file path")
	}
	if !strings.Contains(prompt, "package api") {
		t.Error("missing file content")
	}
}

func TestBuildPromptNilPlan(t *testing.T) {
	prompt := buildPrompt(nil)
	if prompt != "" {
		t.Error("nil plan should return empty string")
	}
}
