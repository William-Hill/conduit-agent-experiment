package triage

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
)

func TestNewTools(t *testing.T) {
	adapter := &github.Adapter{
		Owner: "ConduitIO",
		Repo:  "conduit",
	}
	tools, err := NewTools(adapter, t.TempDir())
	if err != nil {
		t.Fatalf("NewTools() error: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("NewTools() returned %d tools, want 3", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}

	for _, want := range []string{"list_issues", "get_issue", "save_ranking"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}
