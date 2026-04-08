package github

import (
	"context"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
)

// HITLAdapter wraps Adapter to satisfy the hitl.GHAdapter interface,
// converting github types to hitl types.
type HITLAdapter struct {
	*Adapter
}

// GetPRState wraps Adapter.GetPRState, converting github.PRState to hitl.PRState.
func (h *HITLAdapter) GetPRState(ctx context.Context, prNumber int) (*hitl.PRState, error) {
	state, err := h.Adapter.GetPRState(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	return &hitl.PRState{
		State:          state.State,
		IsDraft:        state.IsDraft,
		ReviewDecision: state.ReviewDecision,
	}, nil
}

// GetReviewThreads wraps Adapter.GetReviewThreads, converting types.
func (h *HITLAdapter) GetReviewThreads(ctx context.Context, prNumber int) ([]hitl.ReviewThread, error) {
	threads, err := h.Adapter.GetReviewThreads(ctx, prNumber)
	if err != nil {
		return nil, err
	}
	result := make([]hitl.ReviewThread, len(threads))
	for i, t := range threads {
		result[i] = hitl.ReviewThread{
			ID:         t.ID,
			IsResolved: t.IsResolved,
			Body:       t.Body,
		}
	}
	return result, nil
}
