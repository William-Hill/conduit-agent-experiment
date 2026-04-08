package hitl

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// maxConsecutiveErrors is the number of consecutive API errors tolerated
// before a polling gate gives up. Since gates may wait hours for human
// action, a single transient failure should not abort the pipeline.
const maxConsecutiveErrors = 3

// WaitForLabel polls for any of the target labels on an issue.
// Returns the first matching label found. Blocks until a match or context cancellation.
// Tolerates up to maxConsecutiveErrors transient API failures before aborting.
func WaitForLabel(ctx context.Context, gh GHAdapter, issueNumber int, targetLabels []string, pollInterval time.Duration) (string, error) {
	consecutiveErrors := 0
	for {
		found, err := HasAnyLabel(ctx, gh, issueNumber, targetLabels)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				return "", fmt.Errorf("checking labels on issue #%d (after %d consecutive errors): %w", issueNumber, consecutiveErrors, err)
			}
			log.Printf("[HITL] transient error checking labels on issue #%d (%d/%d): %v", issueNumber, consecutiveErrors, maxConsecutiveErrors, err)
		} else {
			consecutiveErrors = 0
			if found != "" {
				return found, nil
			}
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
// Tolerates up to maxConsecutiveErrors transient API failures before aborting.
func WaitForPRAction(ctx context.Context, gh GHAdapter, prNumber int, pollInterval time.Duration) (string, error) {
	consecutiveErrors := 0
	for {
		state, err := gh.GetPRState(ctx, prNumber)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				return "", fmt.Errorf("checking PR #%d state (after %d consecutive errors): %w", prNumber, consecutiveErrors, err)
			}
			log.Printf("[HITL] transient error checking PR #%d state (%d/%d): %v", prNumber, consecutiveErrors, maxConsecutiveErrors, err)
		} else {
			consecutiveErrors = 0
			if state != nil {
				action := classifyPRAction(state)
				if action != "" {
					return action, nil
				}
			}
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
