package responder

import (
	"testing"
)

func TestClassifyFiltersAddressed(t *testing.T) {
	comments := []ReviewComment{
		{Author: "coderabbitai[bot]", File: "main.go", Line: 10, Body: "Fix this bug", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "main.go", Line: 20, Body: "✅ Addressed in commit abc123", Status: "addressed"},
	}
	result := Classify(comments)
	if len(result) != 1 {
		t.Fatalf("got %d actionable, want 1", len(result))
	}
	if result[0].Line != 10 {
		t.Errorf("expected line 10, got %d", result[0].Line)
	}
}

func TestClassifyFiltersNitpicks(t *testing.T) {
	comments := []ReviewComment{
		{Author: "coderabbitai[bot]", File: "main.go", Line: 10, Body: "_⚠️ Potential issue_ | _🟠 Major_\n\nReal bug here.", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "main.go", Line: 20, Body: "🧹 Nitpick comments\n\nConsider renaming.", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 1 {
		t.Fatalf("got %d actionable, want 1", len(result))
	}
	if result[0].Severity != "major" {
		t.Errorf("severity = %q, want major", result[0].Severity)
	}
}

func TestClassifyNormalizesSeverity(t *testing.T) {
	comments := []ReviewComment{
		{Author: "greptile-apps[bot]", File: "a.go", Line: 1, Body: "P1 **Bug here**", Status: "pending"},
		{Author: "greptile-apps[bot]", File: "b.go", Line: 2, Body: "P2 **Minor issue**", Status: "pending"},
		{Author: "chatgpt-codex-connector[bot]", File: "c.go", Line: 3, Body: "![P1 Badge] **Critical**", Status: "pending"},
		{Author: "coderabbitai[bot]", File: "d.go", Line: 4, Body: "_⚠️ Potential issue_ | _🔴 Critical_\n\nBad.", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 4 {
		t.Fatalf("got %d, want 4", len(result))
	}

	// After sorting: critical (a.go, c.go, d.go) then major (b.go)
	expected := []string{"critical", "critical", "critical", "major"}
	for i, r := range result {
		if r.Severity != expected[i] {
			t.Errorf("comment %d: severity = %q, want %q", i, r.Severity, expected[i])
		}
	}
}

func TestClassifySortsBySeverity(t *testing.T) {
	comments := []ReviewComment{
		{Author: "greptile-apps[bot]", File: "a.go", Line: 1, Body: "P2 **Minor**", Status: "pending"},
		{Author: "greptile-apps[bot]", File: "b.go", Line: 2, Body: "P1 **Critical**", Status: "pending"},
	}
	result := Classify(comments)
	if len(result) != 2 {
		t.Fatalf("got %d, want 2", len(result))
	}
	if result[0].Severity != "critical" {
		t.Errorf("first comment should be critical, got %q", result[0].Severity)
	}
}

func TestClassifyGroupsByFile(t *testing.T) {
	comments := []ReviewComment{
		{Author: "bot", File: "b.go", Line: 10, Body: "P1 fix", Status: "pending"},
		{Author: "bot", File: "a.go", Line: 5, Body: "P1 fix", Status: "pending"},
		{Author: "bot", File: "b.go", Line: 20, Body: "P1 fix2", Status: "pending"},
	}
	result := Classify(comments)
	if result[0].File != "a.go" {
		t.Errorf("expected a.go first (alphabetical grouping), got %q", result[0].File)
	}
	if result[1].File != "b.go" || result[2].File != "b.go" {
		t.Errorf("expected b.go grouped together")
	}
}
