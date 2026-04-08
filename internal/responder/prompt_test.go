package responder

import (
	"strings"
	"testing"
)

func TestBuildFixPrompt(t *testing.T) {
	comments := []ActionableComment{
		{File: "internal/cost/budget.go", Line: 27, Body: "PLANNER_MAX_COST loaded but never enforced", Author: "greptile-apps[bot]", Severity: "critical"},
		{File: "internal/cost/budget.go", Line: 75, Body: "Malformed env var silently returns 0", Author: "greptile-apps[bot]", Severity: "major"},
		{File: "cmd/main.go", Line: 141, Body: "BudgetExceeded flag ignored", Author: "codex[bot]", Severity: "major"},
	}

	prompt := BuildFixPrompt(comments)

	if !strings.Contains(prompt, "internal/cost/budget.go") {
		t.Error("prompt should contain file path")
	}
	if !strings.Contains(prompt, "line 27") {
		t.Error("prompt should contain line number")
	}
	if !strings.Contains(prompt, "PLANNER_MAX_COST") {
		t.Error("prompt should contain comment body")
	}
	if !strings.Contains(prompt, "critical") {
		t.Error("prompt should contain severity")
	}
	if !strings.Contains(prompt, "cmd/main.go") {
		t.Error("prompt should contain second file")
	}
}

func TestBuildFixPromptEmpty(t *testing.T) {
	prompt := BuildFixPrompt(nil)
	if prompt != "" {
		t.Errorf("empty comments should produce empty prompt, got %q", prompt)
	}
}
