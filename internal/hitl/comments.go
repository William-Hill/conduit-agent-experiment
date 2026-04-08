package hitl

import (
	"context"
	"fmt"
)

// TriggerBotReviews posts each trigger comment on a PR to invoke bot reviewers.
func TriggerBotReviews(ctx context.Context, gh GHAdapter, prNumber int, triggers []string) error {
	for _, trigger := range triggers {
		if err := gh.PostComment(ctx, prNumber, trigger); err != nil {
			return fmt.Errorf("posting trigger %q on PR #%d: %w", trigger, prNumber, err)
		}
	}
	return nil
}

// PostTriageRationale posts a comment on an issue explaining why it was selected.
func PostTriageRationale(ctx context.Context, gh GHAdapter, issueNumber int, difficulty, blastRadius string, score int, rationale string) error {
	body := fmt.Sprintf("### 🤖 Agent Triage\n\nThis issue has been selected as a candidate for automated implementation.\n\n| Property | Value |\n|----------|-------|\n| **Difficulty** | %s |\n| **Blast Radius** | %s |\n| **Score** | %d |\n\n**Rationale:** %s\n\n---\nTo approve, add the `agent:approved` label. To reject, add the `agent:rejected` label.",
		difficulty, blastRadius, score, rationale)

	return gh.PostComment(ctx, issueNumber, body)
}
