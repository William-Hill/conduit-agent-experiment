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

// CountHallucinatedSymbols counts identifiers in added diff lines that do
// NOT exist in the given symbol index. To reduce noise the counter skips:
//
//   - Go keywords and builtins (via goKeywords)
//   - Stdlib package names when they appear bare (via stdlibPackages)
//   - The right-hand side of any selector expression (fmt.Println, w.Close,
//     obj.Method) — we cannot resolve these without a type checker, so both
//     stdlib calls and method calls on local variables are passed over.
//   - Identifiers shorter than 3 bytes (one-letter vars, two-char aliases
//     like io, wg, db)
//   - Identifiers found anywhere in string literals will still be scanned,
//     which inflates absolute counts but affects both A/B arms equally.
//
// The metric is approximate and meant for A/B comparison; the delta between
// two backends scored the same way is the load-bearing number, not the
// absolute count.
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
		// Walk idents by position so we can skip the RHS of selector
		// expressions (both pkg.X and obj.X). This avoids a pre-pass
		// and removes the need to guess which LHSes are "stdlib-like".
		for _, loc := range identRe.FindAllStringIndex(body, -1) {
			if loc[0] > 0 && body[loc[0]-1] == '.' {
				continue // selector RHS — already anchored by the LHS
			}
			ident := body[loc[0]:loc[1]]
			if len(ident) < 3 || goKeywords[ident] || stdlibPackages[ident] {
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
