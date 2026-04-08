package responder

import (
	"fmt"
	"strings"
)

// BuildFixPrompt formats actionable comments into a prompt for the fix agent.
func BuildFixPrompt(comments []ActionableComment) string {
	if len(comments) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Review Comments to Address\n\n")
	b.WriteString("Fix each of the following review comments. Make minimal, targeted changes.\n\n")

	for _, c := range comments {
		fmt.Fprintf(&b, "### %s (line %d) [%s, %s]\n\n%s\n\n", c.File, c.Line, c.Author, c.Severity, c.Body)
	}

	return b.String()
}
