package codereviewer

import (
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
)

const codeReviewSystemPrompt = `You are a code review engineer. You receive a GitHub issue, a plan, a research dossier, and a git diff of an attempted implementation. The code already compiles (go build) and passes go vet.

Check ONLY these semantic concerns:
1. Does the diff actually address the issue in the plan?
2. Are there obvious stubs, TODO/FIXME markers, or unfinished code ("... rest of implementation" etc.)?
3. Are there changes to files unrelated to the plan?
4. Does the diff drop or ignore requirements the plan explicitly called out?

Do NOT flag style, naming, test coverage, or "could be cleaner" concerns — those are for CI and human review. Be strict about stubs and missing work; lenient about everything else.

Output ONLY valid JSON:
{"approved": true, "feedback": "Addresses the issue; no stubs"}
or
{"approved": false, "feedback": "File X is referenced in the plan but the diff doesn't touch it. Also main.go:42 has a TODO stub."}`

// buildReviewPrompt assembles the user-side prompt passed to Gemini
// alongside codeReviewSystemPrompt. Order: issue, plan, dossier,
// touched files, diff (diff last because it's the largest section).
func buildReviewPrompt(issue *github.Issue, plan *planner.ImplementationPlan, dossier *archivist.Dossier, diff string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Issue #%d: %s\n\n%s\n\n", issue.Number, issue.Title, issue.Body)
	fmt.Fprintf(&b, "## Plan\n\n%s\n\n", plan.Markdown)
	fmt.Fprintf(&b, "## Research Summary\n\n%s\n\n", dossier.Summary)
	if dossier.Approach != "" {
		fmt.Fprintf(&b, "## Intended Approach\n\n%s\n\n", dossier.Approach)
	}
	b.WriteString("## Touched Files\n\n")
	if len(files) == 0 {
		b.WriteString("(none detected)\n")
	} else {
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	fmt.Fprintf(&b, "\n## Diff\n\n" + "```" + "diff\n%s\n" + "```" + "\n", diff)
	return b.String()
}
