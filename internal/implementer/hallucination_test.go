package implementer

import (
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

// fakeIndex builds an in-memory SymbolIndex for testing.
func fakeIndex(symbols ...string) *ingest.SymbolIndex {
	idx := &ingest.SymbolIndex{}
	for _, name := range symbols {
		idx.Symbols = append(idx.Symbols, ingest.Symbol{Name: name, Kind: "func"})
	}
	return idx
}

func TestCountHallucinatedSymbols_NoHallucinations(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	Existing()
+}
`
	idx := fakeIndex("Bar", "Existing")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("got %d hallucinations, want 0", got)
	}
}

func TestCountHallucinatedSymbols_OneHallucination(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	DoesNotExist()
+}
`
	idx := fakeIndex("Bar")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 1 {
		t.Errorf("got %d hallucinations, want 1", got)
	}
}

func TestCountHallucinatedSymbols_IgnoresStdlib(t *testing.T) {
	diff := `
diff --git a/foo.go b/foo.go
+++ b/foo.go
@@ -1,1 +1,3 @@
+func Bar() {
+	fmt.Println("hi")
+	http.StatusOK
+}
`
	idx := fakeIndex("Bar")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("stdlib refs should not count as hallucinations, got %d", got)
	}
}

func TestCountHallucinatedSymbols_IgnoresKeywords(t *testing.T) {
	diff := `
+++ b/foo.go
+if true {
+	return nil
+}
`
	got := CountHallucinatedSymbols(diff, fakeIndex())
	if got != 0 {
		t.Errorf("keywords should not count, got %d", got)
	}
}

func TestCountHallucinatedSymbols_OnlyAddedLines(t *testing.T) {
	// Removed lines (starting with -) should be ignored.
	diff := `
+++ b/foo.go
-DoesNotExist()
+Existing()
`
	idx := fakeIndex("Existing")
	got := CountHallucinatedSymbols(diff, idx)
	if got != 0 {
		t.Errorf("removed lines should be ignored, got %d", got)
	}
}
