package codereviewer

import (
	"reflect"
	"strings"
	"testing"
)

func TestFilterLintErrors(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		changedFiles []string
		wantKept     []lintError
		wantDropped  int
	}{
		{
			name: "canonical golangci-lint format with mixed files",
			output: "internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
				"internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
				"internal/unchanged/baz.go:11:1: var X should be Y (stylecheck)\n",
			changedFiles: []string{"internal/foo/bar.go"},
			wantKept: []lintError{
				{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
				{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
			},
			wantDropped: 1,
		},
		{
			name:         "no column (line-only)",
			output:       "foo.go:42: some message\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 42, Col: 0, Message: "some message"},
			},
			wantDropped: 0,
		},
		{
			name:         "path normalization strips leading ./",
			output:       "./internal/foo.go:10:1: msg\n",
			changedFiles: []string{"internal/foo.go"},
			wantKept: []lintError{
				{File: "internal/foo.go", Line: 10, Col: 1, Message: "msg"},
			},
			wantDropped: 0,
		},
		{
			name: "non-matching garbage lines are ignored (not counted)",
			output: "level=warning msg=\"something unrelated\"\n" +
				"make: *** [lint] Error 1\n" +
				"foo.go:1:1: real error\n",
			changedFiles: []string{"foo.go"},
			wantKept: []lintError{
				{File: "foo.go", Line: 1, Col: 1, Message: "real error"},
			},
			wantDropped: 0,
		},
		{
			name: "parser cap at 500 lines",
			output: func() string {
				var sb strings.Builder
				for i := 0; i < 1000; i++ {
					sb.WriteString("unchanged.go:1:1: msg\n")
				}
				return sb.String()
			}(),
			changedFiles: []string{"something_else.go"},
			wantKept:     nil,
			wantDropped:  500, // only 500 are parsed; the rest are not seen
		},
		{
			name:         "empty output",
			output:       "",
			changedFiles: []string{"foo.go"},
			wantKept:     nil,
			wantDropped:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped := filterLintErrors(tc.output, tc.changedFiles)
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept:\n  got:  %#v\n  want: %#v", kept, tc.wantKept)
			}
			if dropped != tc.wantDropped {
				t.Errorf("dropped: got %d, want %d", dropped, tc.wantDropped)
			}
		})
	}
}

func TestFormatLintFeedback(t *testing.T) {
	errs := []lintError{
		{File: "internal/foo/bar.go", Line: 42, Col: 5, Message: "ineffectual assignment to err (ineffassign)"},
		{File: "internal/foo/bar.go", Line: 87, Col: 2, Message: "declared and not used: tmp (typecheck)"},
		{File: "cmd/implementer/main.go", Line: 301, Col: 12, Message: "exported function Frob should have comment (golint)"},
	}

	want := "## Lint Errors\n\n" +
		"The following lint violations were introduced by your changes. Fix each one:\n\n" +
		"- internal/foo/bar.go:42:5: ineffectual assignment to err (ineffassign)\n" +
		"- internal/foo/bar.go:87:2: declared and not used: tmp (typecheck)\n" +
		"- cmd/implementer/main.go:301:12: exported function Frob should have comment (golint)\n\n" +
		"Re-run the build and try again."

	got := formatLintFeedback(errs)
	if got != want {
		t.Errorf("formatLintFeedback mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
