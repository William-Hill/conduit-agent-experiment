package hitl

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WaitForLabel polls for any of the target labels on an issue.
// Returns the first matching label found. Blocks until a match or context cancellation.
func WaitForLabel(ctx context.Context, gh GHAdapter, issueNumber int, targetLabels []string, pollInterval time.Duration) (string, error) {
	for {
		found, err := HasAnyLabel(ctx, gh, issueNumber, targetLabels)
		if err != nil {
			return "", fmt.Errorf("checking labels on issue #%d: %w", issueNumber, err)
		}
		if found != "" {
			return found, nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for label on issue #%d: %w", issueNumber, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// WaitForPRAction polls the PR state until a terminal action is detected.
// Returns one of: "merged", "closed", "approved", "changes_requested".
// Blocks until an action or context cancellation.
func WaitForPRAction(ctx context.Context, gh GHAdapter, prNumber int, pollInterval time.Duration) (string, error) {
	for {
		state, err := gh.GetPRState(ctx, prNumber)
		if err != nil {
			return "", fmt.Errorf("checking PR #%d state: %w", prNumber, err)
		}

		action := classifyPRAction(state)
		if action != "" {
			return action, nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("waiting for PR #%d action: %w", prNumber, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func classifyPRAction(state *PRState) string {
	switch strings.ToUpper(state.State) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}

	switch strings.ToUpper(state.ReviewDecision) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes_requested"
	}

	return ""
}
