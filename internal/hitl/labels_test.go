package hitl

import (
	"context"
	"testing"
)

// mockAdapter implements the GHAdapter interface for testing.
type mockAdapter struct {
	labels          []string
	addedLabels     []string
	removedLabels   []string
	comments        []string
	prState         *PRState
	addLabelErr     error
	threads         []ReviewThread
	resolvedThreads []string
}

func (m *mockAdapter) AddLabel(_ context.Context, _ int, label string) error {
	if m.addLabelErr != nil {
		return m.addLabelErr
	}
	m.addedLabels = append(m.addedLabels, label)
	m.labels = append(m.labels, label)
	return nil
}

func (m *mockAdapter) RemoveLabel(_ context.Context, _ int, label string) error {
	m.removedLabels = append(m.removedLabels, label)
	return nil
}

func (m *mockAdapter) GetLabels(_ context.Context, _ int) ([]string, error) {
	return m.labels, nil
}

func (m *mockAdapter) PostComment(_ context.Context, _ int, body string) error {
	m.comments = append(m.comments, body)
	return nil
}

func (m *mockAdapter) GetPRState(_ context.Context, _ int) (*PRState, error) {
	return m.prState, nil
}

func (m *mockAdapter) GetReviewThreads(_ context.Context, _ int) ([]ReviewThread, error) {
	return m.threads, nil
}

func (m *mockAdapter) ResolveThread(_ context.Context, threadID string) error {
	m.resolvedThreads = append(m.resolvedThreads, threadID)
	return nil
}

func TestHasLabel(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug", "agent:candidate"}}

	has, err := HasLabel(context.Background(), mock, 42, "agent:candidate")
	if err != nil {
		t.Fatalf("HasLabel() error: %v", err)
	}
	if !has {
		t.Error("HasLabel() = false, want true")
	}

	has, err = HasLabel(context.Background(), mock, 42, "agent:approved")
	if err != nil {
		t.Fatalf("HasLabel() error: %v", err)
	}
	if has {
		t.Error("HasLabel() = true, want false")
	}
}

func TestHasAnyLabel(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug", "agent:approved"}}

	found, err := HasAnyLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"})
	if err != nil {
		t.Fatalf("HasAnyLabel() error: %v", err)
	}
	if found != "agent:approved" {
		t.Errorf("HasAnyLabel() = %q, want %q", found, "agent:approved")
	}
}

func TestHasAnyLabel_NoneFound(t *testing.T) {
	mock := &mockAdapter{labels: []string{"bug"}}

	found, err := HasAnyLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"})
	if err != nil {
		t.Fatalf("HasAnyLabel() error: %v", err)
	}
	if found != "" {
		t.Errorf("HasAnyLabel() = %q, want empty string", found)
	}
}

