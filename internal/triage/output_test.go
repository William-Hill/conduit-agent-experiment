package triage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRanking(t *testing.T) {
	dir := t.TempDir()

	output := TriageOutput{
		Timestamp:   "2026-04-06T12:00:00Z",
		Repo:        "ConduitIO/conduit",
		TotalIssues: 130,
		Ranked: []RankedIssue{
			{
				Number:      576,
				Title:       "HTTP status codes should be named constants",
				Category:    "housekeeping",
				Difficulty:  "L2",
				BlastRadius: "low",
				Feasibility: 8,
				Demand:      6,
				Score:       48,
				Rationale:   "Clear scope, low risk, testable",
				Suitable:    true,
			},
		},
	}

	path, err := SaveRanking(dir, output)
	if err != nil {
		t.Fatalf("SaveRanking() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	var got TriageOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parsing output JSON: %v", err)
	}

	if got.Repo != "ConduitIO/conduit" {
		t.Errorf("Repo = %q, want %q", got.Repo, "ConduitIO/conduit")
	}
	if len(got.Ranked) != 1 {
		t.Fatalf("Ranked length = %d, want 1", len(got.Ranked))
	}
	if got.Ranked[0].Number != 576 {
		t.Errorf("Ranked[0].Number = %d, want 576", got.Ranked[0].Number)
	}
	if got.Ranked[0].Score != 48 {
		t.Errorf("Ranked[0].Score = %d, want 48", got.Ranked[0].Score)
	}

	// Verify filename contains the date
	base := filepath.Base(path)
	if base != "triage-2026-04-06.json" {
		t.Errorf("filename = %q, want %q", base, "triage-2026-04-06.json")
	}
}
