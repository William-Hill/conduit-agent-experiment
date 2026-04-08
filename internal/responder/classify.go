package responder

import (
	"sort"
	"strings"
)

// ActionableComment is a review comment that needs a code fix.
type ActionableComment struct {
	File     string
	Line     int
	Body     string
	Author   string
	Severity string // "critical", "major", "minor"
}

// Classify filters out addressed and nitpick comments, normalizes severity,
// and groups remaining actionable comments by file, sorted by severity.
func Classify(comments []ReviewComment) []ActionableComment {
	var result []ActionableComment

	for _, c := range comments {
		if c.Status == "addressed" {
			continue
		}
		sev := extractSeverity(c.Body, c.Author)
		if sev == "nitpick" || sev == "skip" {
			continue
		}
		result = append(result, ActionableComment{
			File:     c.File,
			Line:     c.Line,
			Body:     c.Body,
			Author:   c.Author,
			Severity: sev,
		})
	}

	sevOrder := map[string]int{"critical": 0, "major": 1, "minor": 2}
	sort.Slice(result, func(i, j int) bool {
		si, sj := sevOrder[result[i].Severity], sevOrder[result[j].Severity]
		if si != sj {
			return si < sj
		}
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		return result[i].Line < result[j].Line
	})

	return result
}

// extractSeverity normalizes severity across Greptile, CodeRabbit, and Codex.
func extractSeverity(body, author string) string {
	lower := strings.ToLower(body)

	if strings.Contains(lower, "nitpick") {
		return "nitpick"
	}
	if strings.Contains(lower, "walkthrough") || strings.Contains(lower, "📝 walkthrough") {
		return "skip"
	}

	if strings.Contains(body, "P1") || strings.Contains(body, "p1.svg") {
		return "critical"
	}
	if strings.Contains(body, "P2") || strings.Contains(body, "p2.svg") {
		return "major"
	}
	if strings.Contains(body, "P3") || strings.Contains(body, "p3.svg") {
		return "nitpick"
	}

	if strings.Contains(lower, "🔴 critical") || strings.Contains(lower, "critical_") {
		return "critical"
	}
	if strings.Contains(lower, "🟠 major") {
		return "major"
	}
	if strings.Contains(lower, "🟡 minor") {
		return "minor"
	}

	if strings.Contains(body, "P1-") {
		return "critical"
	}
	if strings.Contains(body, "P2-") {
		return "major"
	}

	if strings.Contains(body, "⚠️") || strings.Contains(lower, "potential issue") {
		return "major"
	}

	return "minor"
}
