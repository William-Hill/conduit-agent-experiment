package codereviewer

import (
	"regexp"
	"strconv"
	"strings"
)

// lintError is a single parsed diagnostic from a Go linter's output.
// Col is 0 when the linter emitted only file:line without a column.
type lintError struct {
	File    string
	Line    int
	Col     int
	Message string
}

// lintParseCap bounds how many error lines the parser will accept before
// giving up. Pathological output (thousands of errors from a misconfigured
// lint run) should not balloon memory or CPU.
const lintParseCap = 500

// lintLineRE matches the canonical Go linter output format:
//
//	file:line:col: message
//	file:line: message
//
// It deliberately rejects lines starting with non-file prefixes like
// "level=" or "make:" by anchoring file to a non-colon run followed by
// a required :<digits>: pattern.
var lintLineRE = regexp.MustCompile(`^([^:]+):(\d+):(?:(\d+):)?\s*(.+)$`)

// filterLintErrors parses lint output and returns only errors whose file
// path falls within changedFiles. The second return is the count of
// parsed-but-dropped errors (errors in unchanged files — pre-existing
// debt we do not want to retry on).
//
// Lines that do not match lintLineRE are ignored entirely, not counted
// as dropped, because they were never parsed as errors. Parsing stops
// after lintParseCap matched lines.
func filterLintErrors(output string, changedFiles []string) ([]lintError, int) {
	changed := make(map[string]struct{}, len(changedFiles))
	for _, f := range changedFiles {
		changed[normalizeLintPath(f)] = struct{}{}
	}

	var kept []lintError
	dropped := 0
	parsed := 0

	for _, line := range strings.Split(output, "\n") {
		if parsed >= lintParseCap {
			break
		}
		m := lintLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		parsed++

		file := normalizeLintPath(m[1])
		lineNum, _ := strconv.Atoi(m[2])
		col := 0
		if m[3] != "" {
			col, _ = strconv.Atoi(m[3])
		}
		msg := m[4]

		if _, ok := changed[file]; ok {
			kept = append(kept, lintError{
				File:    file,
				Line:    lineNum,
				Col:     col,
				Message: msg,
			})
		} else {
			dropped++
		}
	}

	return kept, dropped
}

// normalizeLintPath strips a leading "./" so that a linter emitting
// "./internal/foo.go" matches a changedFiles entry of "internal/foo.go".
// Absolute-path normalization against repoDir is handled by the caller
// before passing paths into filterLintErrors.
func normalizeLintPath(p string) string {
	return strings.TrimPrefix(p, "./")
}

// formatLintFeedback renders a slice of lintError records into the
// markdown body that Review() stores in verdict.Feedback on a lint
// rejection. cmd/implementer/main.go appends verdict.Feedback to the
// retry plan unchanged, so the format here is what the implementer
// actually reads on the retry pass.
//
// Col == 0 is rendered without the trailing ":0" so line-only linter
// output reads naturally. (The current v1 caller always passes col > 0
// from the canonical golangci-lint format, but the fallback keeps the
// output clean if a future linter omits columns.)
func formatLintFeedback(errs []lintError) string {
	var b strings.Builder
	b.WriteString("## Lint Errors\n\n")
	b.WriteString("The following lint violations were introduced by your changes. Fix each one:\n\n")
	for _, e := range errs {
		if e.Col > 0 {
			b.WriteString("- " + e.File + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ": " + e.Message + "\n")
		} else {
			b.WriteString("- " + e.File + ":" + strconv.Itoa(e.Line) + ": " + e.Message + "\n")
		}
	}
	b.WriteString("\nRe-run the build and try again.")
	return b.String()
}
