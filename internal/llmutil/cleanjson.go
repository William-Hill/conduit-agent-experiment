package llmutil

import (
	"regexp"
	"strings"
)

var fencedJSONRE = regexp.MustCompile("(?s)^```[A-Za-z0-9_+-]*\\s*(.*?)\\s*```$")

// CleanJSON strips a single outer markdown code fence from a JSON response.
// Only removes a complete fence (opening ``` + optional language + closing ```).
func CleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if m := fencedJSONRE.FindStringSubmatch(s); len(m) == 2 {
		s = m[1]
	}
	return strings.TrimSpace(s)
}
