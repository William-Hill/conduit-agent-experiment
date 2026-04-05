package evaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

func TestBuildEvaluation(t *testing.T) {
	input := EvalInput{
		RunID:               "run-abc",
		TaskID:              "task-001",
		IssueNumber:         42,
		Difficulty:          "L1",
		BlastRadius:         "low",
		TriageDecision:      "accept",
		ImplementerSuccess:  true,
		FilesChanged:        3,
		DiffLines:           55,
		VerifierPass:        true,
		ArchitectDecision:   "approve",
		ArchitectConfidence: "high",
		PRCreated:           true,
		PRURL:               "https://github.com/org/repo/pull/7",
		FailureMode:         models.FailureHallucination,
		FailureDetail:       "agent invented a function",
		TotalDurationMs:     12345,
		LLMCalls:            8,
		LLMTokensUsed:       2048,
	}

	eval := BuildEvaluation(input)

	if eval.RunID != input.RunID {
		t.Errorf("RunID: got %q, want %q", eval.RunID, input.RunID)
	}
	if eval.TaskID != input.TaskID {
		t.Errorf("TaskID: got %q, want %q", eval.TaskID, input.TaskID)
	}
	if eval.IssueNumber != input.IssueNumber {
		t.Errorf("IssueNumber: got %d, want %d", eval.IssueNumber, input.IssueNumber)
	}
	if eval.Difficulty != input.Difficulty {
		t.Errorf("Difficulty: got %q, want %q", eval.Difficulty, input.Difficulty)
	}
	if eval.BlastRadius != input.BlastRadius {
		t.Errorf("BlastRadius: got %q, want %q", eval.BlastRadius, input.BlastRadius)
	}
	if eval.TriageDecision != input.TriageDecision {
		t.Errorf("TriageDecision: got %q, want %q", eval.TriageDecision, input.TriageDecision)
	}
	if eval.ImplementerSuccess != input.ImplementerSuccess {
		t.Errorf("ImplementerSuccess: got %v, want %v", eval.ImplementerSuccess, input.ImplementerSuccess)
	}
	if eval.FilesChanged != input.FilesChanged {
		t.Errorf("FilesChanged: got %d, want %d", eval.FilesChanged, input.FilesChanged)
	}
	if eval.DiffLines != input.DiffLines {
		t.Errorf("DiffLines: got %d, want %d", eval.DiffLines, input.DiffLines)
	}
	if eval.VerifierPass != input.VerifierPass {
		t.Errorf("VerifierPass: got %v, want %v", eval.VerifierPass, input.VerifierPass)
	}
	if eval.ArchitectDecision != input.ArchitectDecision {
		t.Errorf("ArchitectDecision: got %q, want %q", eval.ArchitectDecision, input.ArchitectDecision)
	}
	if eval.ArchitectConfidence != input.ArchitectConfidence {
		t.Errorf("ArchitectConfidence: got %q, want %q", eval.ArchitectConfidence, input.ArchitectConfidence)
	}
	if eval.PRCreated != input.PRCreated {
		t.Errorf("PRCreated: got %v, want %v", eval.PRCreated, input.PRCreated)
	}
	if eval.PRURL != input.PRURL {
		t.Errorf("PRURL: got %q, want %q", eval.PRURL, input.PRURL)
	}
	if eval.FailureMode != input.FailureMode {
		t.Errorf("FailureMode: got %q, want %q", eval.FailureMode, input.FailureMode)
	}
	if eval.FailureDetail != input.FailureDetail {
		t.Errorf("FailureDetail: got %q, want %q", eval.FailureDetail, input.FailureDetail)
	}
	if eval.TotalDurationMs != input.TotalDurationMs {
		t.Errorf("TotalDurationMs: got %d, want %d", eval.TotalDurationMs, input.TotalDurationMs)
	}
	if eval.LLMCalls != input.LLMCalls {
		t.Errorf("LLMCalls: got %d, want %d", eval.LLMCalls, input.LLMCalls)
	}
	if eval.LLMTokensUsed != input.LLMTokensUsed {
		t.Errorf("LLMTokensUsed: got %d, want %d", eval.LLMTokensUsed, input.LLMTokensUsed)
	}
}

func TestWriteAndLoadEvaluation(t *testing.T) {
	dir := t.TempDir()

	eval := models.Evaluation{
		RunID:              "run-xyz",
		TaskID:             "task-002",
		IssueNumber:        7,
		Difficulty:         "L2",
		BlastRadius:        "medium",
		TriageDecision:     "accept",
		ImplementerSuccess: true,
		FilesChanged:       2,
		DiffLines:          30,
		VerifierPass:       true,
		PRCreated:          false,
		TotalDurationMs:    9000,
		LLMCalls:           5,
	}

	if err := WriteEvaluationJSON(dir, eval); err != nil {
		t.Fatalf("WriteEvaluationJSON() error: %v", err)
	}

	path := filepath.Join(dir, "evaluation.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading evaluation.json: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("evaluation.json is empty")
	}

	var loaded models.Evaluation
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshalling evaluation.json: %v", err)
	}
	if loaded.RunID != eval.RunID {
		t.Errorf("loaded RunID: got %q, want %q", loaded.RunID, eval.RunID)
	}
	if loaded.TaskID != eval.TaskID {
		t.Errorf("loaded TaskID: got %q, want %q", loaded.TaskID, eval.TaskID)
	}
	if loaded.TotalDurationMs != eval.TotalDurationMs {
		t.Errorf("loaded TotalDurationMs: got %d, want %d", loaded.TotalDurationMs, eval.TotalDurationMs)
	}
}
