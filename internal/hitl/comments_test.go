package hitl

import (
	"context"
	"strings"
	"testing"
)

func TestTriggerBotReviews(t *testing.T) {
	mock := &mockAdapter{}

	triggers := []string{"@coderabbitai review", "@greptile review"}
	if err := TriggerBotReviews(context.Background(), mock, 42, triggers); err != nil {
		t.Fatalf("TriggerBotReviews() error: %v", err)
	}

	if len(mock.comments) != 2 {
		t.Fatalf("posted %d comments, want 2", len(mock.comments))
	}
	if mock.comments[0] != "@coderabbitai review" {
		t.Errorf("comments[0] = %q, want %q", mock.comments[0], "@coderabbitai review")
	}
	if mock.comments[1] != "@greptile review" {
		t.Errorf("comments[1] = %q, want %q", mock.comments[1], "@greptile review")
	}
}

func TestResolveAddressedThreads(t *testing.T) {
	mock := &mockAdapter{
		threads: []ReviewThread{
			{ID: "RT_1", IsResolved: false, Body: "Fix this typo"},
			{ID: "RT_2", IsResolved: true, Body: "Already resolved"},
			{ID: "RT_3", IsResolved: false, Body: "Another issue"},
		},
	}

	resolved, err := ResolveAddressedThreads(context.Background(), mock, 42)
	if err != nil {
		t.Fatalf("ResolveAddressedThreads() error: %v", err)
	}

	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}
	if len(mock.resolvedThreads) != 2 {
		t.Fatalf("resolvedThreads = %v, want [RT_1, RT_3]", mock.resolvedThreads)
	}
}

func TestResolveAddressedThreads_NoneUnresolved(t *testing.T) {
	mock := &mockAdapter{
		threads: []ReviewThread{
			{ID: "RT_1", IsResolved: true, Body: "Done"},
		},
	}

	resolved, err := ResolveAddressedThreads(context.Background(), mock, 42)
	if err != nil {
		t.Fatalf("ResolveAddressedThreads() error: %v", err)
	}
	if resolved != 0 {
		t.Errorf("resolved = %d, want 0", resolved)
	}
}

func TestPostTriageRationale(t *testing.T) {
	mock := &mockAdapter{}

	if err := PostTriageRationale(context.Background(), mock, 42, "L1", "low", 85, "Simple doc fix"); err != nil {
		t.Fatalf("PostTriageRationale() error: %v", err)
	}

	if len(mock.comments) != 1 {
		t.Fatalf("posted %d comments, want 1", len(mock.comments))
	}

	comment := mock.comments[0]
	if !strings.Contains(comment, "Agent Triage") {
		t.Error("comment should contain 'Agent Triage'")
	}
	if !strings.Contains(comment, "L1") {
		t.Error("comment should contain difficulty")
	}
	if !strings.Contains(comment, "low") {
		t.Error("comment should contain blast radius")
	}
	if !strings.Contains(comment, "85") {
		t.Error("comment should contain score")
	}
}
