package triage

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
)

// stubLLM implements model.LLM for testing. It is never called during
// construction — the agent only contacts the model at runtime.
type stubLLM struct{}

func (s *stubLLM) Name() string { return "stub-model" }

func (s *stubLLM) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {}
}

func TestNewTriageAgent(t *testing.T) {
	adapter := &github.Adapter{Owner: "ConduitIO", Repo: "conduit"}
	tools, err := NewTools(adapter, t.TempDir())
	if err != nil {
		t.Fatalf("NewTools() error: %v", err)
	}

	agent, err := NewTriageAgent(&stubLLM{}, tools)
	if err != nil {
		t.Fatalf("NewTriageAgent() error: %v", err)
	}
	if agent.Name() != "triage_agent" {
		t.Errorf("agent.Name() = %q, want %q", agent.Name(), "triage_agent")
	}
	if agent.Description() != "Scans GitHub issues and produces a ranked queue of tasks suitable for automated maintenance." {
		t.Errorf("agent.Description() = %q, unexpected", agent.Description())
	}
}

func TestNewTriageAgentNilModel(t *testing.T) {
	// Passing nil model should still succeed at construction time;
	// the model is only used at runtime when the agent is invoked.
	agent, err := NewTriageAgent(nil, []tool.Tool{})
	if err != nil {
		t.Fatalf("NewTriageAgent(nil, ...) error: %v", err)
	}
	if agent.Name() != "triage_agent" {
		t.Errorf("agent.Name() = %q, want %q", agent.Name(), "triage_agent")
	}
}
