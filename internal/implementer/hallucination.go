package implementer

import (
	"regexp"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

// goKeywords lists Go keywords and common builtin identifiers. They're
// filtered out before symbol lookup.
var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
	"true": true, "false": true, "nil": true, "iota": true,
	"string": true, "int": true, "int32": true, "int64": true, "uint": true,
	"uint32": true, "uint64": true, "float32": true, "float64": true, "bool": true,
	"byte": true, "rune": true, "error": true, "any": true,
	"make": true, "new": true, "len": true, "cap": true, "append": true,
	"copy": true, "delete": true, "panic": true, "recover": true, "print": true,
	"println": true, "close": true,
}

// stdlibPackages is a non-exhaustive list of Go stdlib package selectors
// that appear at the start of qualified identifiers. Matches are treated
// as real references rather than hallucinations.
var stdlibPackages = map[string]bool{
	"fmt": true, "os": true, "io": true, "context": true, "errors": true,
	"http": true, "json": true, "time": true, "strings": true, "strconv": true,
	"bytes": true, "log": true, "sync": true, "sort": true, "regexp": true,
	"path": true, "filepath": true, "exec": true, "testing": true, "reflect": true,
	"bufio": true, "unicode": true, "math": true, "url": true,
}

// identRe matches Go identifiers — letters, digits, underscores, starting with
// a letter or underscore.
var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// qualifiedRe matches stdlib-qualified selectors like fmt.Println or http.StatusOK.
// We pre-collect these so their right-hand sides are not counted as hallucinations.
var qualifiedRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*`)

// CountHallucinatedSymbols counts identifiers in added diff lines that do
// NOT exist in the given symbol index. Stdlib package selectors, Go keywords,
// builtins, and identifiers shorter than 3 characters are ignored.
//
// The metric is approximate — the goal is A/B comparison, not absolute
// correctness. Both arms are scored the same way.
func CountHallucinatedSymbols(diff string, idx *ingest.SymbolIndex) int {
	if idx == nil {
		return 0
	}
	seen := make(map[string]bool)
	count := 0
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		body := line[1:]
		// Collect identifiers that are the right-hand side of a stdlib-qualified
		// selector (e.g. "Println" in "fmt.Println"). These should not be flagged.
		qualifiedSkip := make(map[string]bool)
		for _, qm := range qualifiedRe.FindAllString(body, -1) {
			parts := strings.SplitN(qm, ".", 2)
			if len(parts) == 2 && stdlibPackages[parts[0]] {
				qualifiedSkip[parts[1]] = true
			}
		}
		for _, ident := range identRe.FindAllString(body, -1) {
			if len(ident) < 3 || goKeywords[ident] || stdlibPackages[ident] || qualifiedSkip[ident] {
				continue
			}
			if seen[ident] {
				continue
			}
			seen[ident] = true
			if len(ingest.SearchSymbols(idx, ident)) == 0 {
				count++
			}
		}
	}
	return count
}
