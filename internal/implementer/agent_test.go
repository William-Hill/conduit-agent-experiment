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
		Markdown: "# Fix error codes\n\nChange `pkg/api.go` to return proper status codes.\n\n```go\npackage api\n```\n",
	}
	prompt := buildPrompt(p)
	if !strings.Contains(prompt, "pkg/api.go") {
		t.Error("missing file path")
	}
	if !strings.Contains(prompt, "package api") {
		t.Error("missing code content")
	}
}

func TestBuildPromptNilPlan(t *testing.T) {
	prompt := buildPrompt(nil)
	if prompt != "" {
		t.Error("nil plan should return empty string")
	}
}

func TestResultTokenFields(t *testing.T) {
	r := Result{
		Summary:      "wrote 2 files",
		Iterations:   3,
		InputTokens:  1500,
		OutputTokens: 800,
	}
	if r.InputTokens != 1500 {
		t.Errorf("InputTokens = %d, want 1500", r.InputTokens)
	}
	if r.OutputTokens != 800 {
		t.Errorf("OutputTokens = %d, want 800", r.OutputTokens)
	}
}
