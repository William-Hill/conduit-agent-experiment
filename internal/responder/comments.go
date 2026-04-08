package responder

import (
	"encoding/json"
	"strings"
)

// ReviewComment represents a single inline review comment from any tool.
type ReviewComment struct {
	Author string
	File   string
	Line   int
	Body   string
	Status string // "pending", "addressed"
}

// ghInlineComment mirrors the GitHub API JSON shape for PR review comments.
type ghInlineComment struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// ghReview mirrors the GitHub API JSON shape for PR reviews.
type ghReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
}

// ParseInlineComments parses the JSON output of gh api .../pulls/{n}/comments.
func ParseInlineComments(data []byte) ([]ReviewComment, error) {
	var raw []ghInlineComment
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	comments := make([]ReviewComment, 0, len(raw))
	for _, c := range raw {
		status := "pending"
		if isAddressed(c.Body) {
			status = "addressed"
		}
		comments = append(comments, ReviewComment{
			Author: c.User.Login,
			File:   c.Path,
			Line:   c.Line,
			Body:   c.Body,
			Status: status,
		})
	}
	return comments, nil
}

// HasApproval parses the JSON output of gh pr view --json reviews and returns
// true if the latest review from any author is "APPROVED" and no author's
// latest review is "CHANGES_REQUESTED".
func HasApproval(data []byte) (bool, error) {
	var reviews []ghReview
	if err := json.Unmarshal(data, &reviews); err != nil {
		return false, err
	}
	// Track the most recent state per author (reviews are ordered chronologically).
	latest := make(map[string]string)
	for _, r := range reviews {
		latest[r.Author.Login] = r.State
	}
	approved := false
	for _, state := range latest {
		if state == "CHANGES_REQUESTED" {
			return false, nil
		}
		if state == "APPROVED" {
			approved = true
		}
	}
	return approved, nil
}

// isAddressed detects comments that review tools have marked as resolved.
func isAddressed(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "addressed in commit") ||
		strings.Contains(lower, "✅ addressed")
}
