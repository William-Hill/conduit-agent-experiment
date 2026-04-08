package responder

import (
	"fmt"
	"strings"
)

const fixSystemPrompt = `You are a code review response agent. You receive review comments from
automated reviewers and must fix each one with minimal, targeted changes.

For each comment:
1. Read the file mentioned in the comment
2. Understand what the reviewer is asking for
3. Make the minimal fix
4. Run "go build ./..." to verify the fix compiles

Do NOT refactor surrounding code. Do NOT add features. Fix exactly what
the reviewer flagged, nothing more. After fixing all comments, run
"go build ./..." one final time and state what you changed.`

// BuildFixPrompt formats actionable comments into a prompt for the fix agent.
func BuildFixPrompt(comments []ActionableComment) string {
	if len(comments) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Review Comments to Address\n\n")
	b.WriteString("Fix each of the following review comments. Make minimal, targeted changes.\n\n")

	currentFile := ""
	for _, c := range comments {
		if c.File != currentFile {
			if currentFile != "" {
				b.WriteString("\n")
			}
			currentFile = c.File
		}
		fmt.Fprintf(&b, "### %s (line %d) [%s, %s]\n\n%s\n\n", c.File, c.Line, c.Author, c.Severity, c.Body)
	}

	return b.String()
}
