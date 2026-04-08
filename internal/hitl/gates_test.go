package hitl

import (
	"context"
	"testing"
	"time"
)

// pollCountAdapter tracks how many times GetLabels/GetPRState were called.
type pollCountAdapter struct {
	mockAdapter
	labelPollCount int
	prPollCount    int
	labelsSequence [][]string
	prSequence     []*PRState
}

func (p *pollCountAdapter) GetLabels(_ context.Context, _ int) ([]string, error) {
	idx := p.labelPollCount
	p.labelPollCount++
	if idx < len(p.labelsSequence) {
		return p.labelsSequence[idx], nil
	}
	return p.labelsSequence[len(p.labelsSequence)-1], nil
}

func (p *pollCountAdapter) GetPRState(_ context.Context, _ int) (*PRState, error) {
	idx := p.prPollCount
	p.prPollCount++
	if idx < len(p.prSequence) {
		return p.prSequence[idx], nil
	}
	return p.prSequence[len(p.prSequence)-1], nil
}

func TestWaitForLabel_ImmediateMatch(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{{"bug", "agent:approved"}},
	}

	label, err := WaitForLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"}, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForLabel() error: %v", err)
	}
	if label != "agent:approved" {
		t.Errorf("WaitForLabel() = %q, want %q", label, "agent:approved")
	}
	if mock.labelPollCount != 1 {
		t.Errorf("polled %d times, want 1", mock.labelPollCount)
	}
}

func TestWaitForLabel_PollsUntilMatch(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{
			{"bug"},
			{"bug"},
			{"bug", "agent:rejected"},
		},
	}

	label, err := WaitForLabel(context.Background(), mock, 42, []string{"agent:approved", "agent:rejected"}, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForLabel() error: %v", err)
	}
	if label != "agent:rejected" {
		t.Errorf("WaitForLabel() = %q, want %q", label, "agent:rejected")
	}
	if mock.labelPollCount != 3 {
		t.Errorf("polled %d times, want 3", mock.labelPollCount)
	}
}

func TestWaitForLabel_ContextCancelled(t *testing.T) {
	mock := &pollCountAdapter{
		labelsSequence: [][]string{{"bug"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := WaitForLabel(ctx, mock, 42, []string{"agent:approved"}, 10*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForLabel() should return error on context cancellation")
	}
}

func TestWaitForPRAction_Merged(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{{State: "MERGED", IsDraft: false}},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "merged" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "merged")
	}
}

func TestWaitForPRAction_Closed(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{{State: "CLOSED", IsDraft: false}},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "closed" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "closed")
	}
}

func TestWaitForPRAction_ChangesRequested(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{
			{State: "OPEN", IsDraft: true, ReviewDecision: "REVIEW_REQUIRED"},
			{State: "OPEN", IsDraft: true, ReviewDecision: "CHANGES_REQUESTED"},
		},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "changes_requested" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "changes_requested")
	}
}

func TestWaitForPRAction_Approved(t *testing.T) {
	mock := &pollCountAdapter{
		prSequence: []*PRState{{State: "OPEN", IsDraft: true, ReviewDecision: "APPROVED"}},
	}

	action, err := WaitForPRAction(context.Background(), mock, 42, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPRAction() error: %v", err)
	}
	if action != "approved" {
		t.Errorf("WaitForPRAction() = %q, want %q", action, "approved")
	}
}
