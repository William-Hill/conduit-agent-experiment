package codereviewer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// lintConfig describes which linter RunLint should invoke.
type lintConfig struct {
	Mode       string // "make" or "golangci-lint"
	ConfigPath string // empty for make mode; path to .golangci.* for golangci-lint mode
}

// lintMakefileScanCap bounds how many bytes of a target repo's Makefile
// we inspect when probing for a lint target. 64 KiB is vastly larger
// than any real Makefile and small enough that a pathological fixture
// cannot blow up memory or CPU.
const lintMakefileScanCap = 64 * 1024

// lintMakeTargetRE matches a line like `lint:` or `lint: deps` at the
// beginning of a line. \n anchors so we do not match `prelint:` etc.
var lintMakeTargetRE = regexp.MustCompile(`(?m)^lint:`)

// lookPathFn is exec.LookPath by default, exposed as a package var so
// tests can stub binary discovery without touching the host PATH.
var lookPathFn = exec.LookPath

// detectLinter probes repoDir for the linter RunLint should use.
// Returns (nil, nil) when no lint workflow is available — callers
// should treat this as a silent no-op, not an error.
//
// Order of precedence:
//  1. AGENT_LINT=off env var short-circuits to nil (operator kill switch)
//  2. Makefile with a `^lint:` target → {Mode: "make"}
//  3. golangci-lint binary on PATH AND a .golangci.{yml,yaml,toml}
//     config in repoDir → {Mode: "golangci-lint", ConfigPath: ...}
//  4. Otherwise → nil
func detectLinter(repoDir string) (*lintConfig, error) {
	if strings.EqualFold(os.Getenv("AGENT_LINT"), "off") {
		return nil, nil
	}

	// Makefile probe. Read bounded prefix to protect against pathological
	// Makefiles (and for symmetry with the rest of the package, which
	// caps all external input).
	if f, err := os.Open(filepath.Join(repoDir, "Makefile")); err == nil {
		buf := make([]byte, lintMakefileScanCap)
		n, rerr := io.ReadFull(f, buf)
		f.Close()
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("reading Makefile: %w", rerr)
		}
		if lintMakeTargetRE.Match(buf[:n]) {
			return &lintConfig{Mode: "make"}, nil
		}
	}

	// golangci-lint probe. Requires both the binary AND a config file —
	// running golangci-lint without a config in a repo that hasn't opted
	// into it would be presumptuous and noisy.
	for _, name := range []string{".golangci.yml", ".golangci.yaml", ".golangci.toml"} {
		cfgPath := filepath.Join(repoDir, name)
		if _, err := os.Stat(cfgPath); err == nil {
			if _, err := lookPathFn("golangci-lint"); err == nil {
				return &lintConfig{Mode: "golangci-lint", ConfigPath: cfgPath}, nil
			}
			// Config found but binary missing — fall through to nil so
			// the pipeline skips lint rather than halting.
			break
		}
	}

	return nil, nil
}
