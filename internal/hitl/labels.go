package hitl

import "context"

// GHAdapter defines the GitHub operations needed by HITL gates.
type GHAdapter interface {
	AddLabel(ctx context.Context, number int, label string) error
	RemoveLabel(ctx context.Context, number int, label string) error
	GetLabels(ctx context.Context, number int) ([]string, error)
	PostComment(ctx context.Context, number int, body string) error
	GetPRState(ctx context.Context, prNumber int) (*PRState, error)
}

// PRState represents the current state of a pull request.
type PRState struct {
	State          string `json:"state"`          // OPEN, CLOSED, MERGED
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
}

// Label constants used by HITL gates.
const (
	LabelCandidate      = "agent:candidate"
	LabelApproved       = "agent:approved"
	LabelRejected       = "agent:rejected"
	LabelReadyForReview = "agent:ready-for-review"
)

// ApplyLabel adds a label to an issue or PR.
func ApplyLabel(ctx context.Context, gh GHAdapter, number int, label string) error {
	return gh.AddLabel(ctx, number, label)
}

// RemoveLabel removes a label from an issue or PR.
func RemoveLabel(ctx context.Context, gh GHAdapter, number int, label string) error {
	return gh.RemoveLabel(ctx, number, label)
}

// HasLabel checks if an issue or PR has a specific label.
func HasLabel(ctx context.Context, gh GHAdapter, number int, label string) (bool, error) {
	labels, err := gh.GetLabels(ctx, number)
	if err != nil {
		return false, err
	}
	for _, l := range labels {
		if l == label {
			return true, nil
		}
	}
	return false, nil
}

// HasAnyLabel checks if an issue or PR has any of the given labels.
// Returns the first matching label found, or empty string if none match.
func HasAnyLabel(ctx context.Context, gh GHAdapter, number int, targets []string) (string, error) {
	labels, err := gh.GetLabels(ctx, number)
	if err != nil {
		return "", err
	}
	labelSet := make(map[string]bool, len(labels))
	for _, l := range labels {
		labelSet[l] = true
	}
	for _, target := range targets {
		if labelSet[target] {
			return target, nil
		}
	}
	return "", nil
}
