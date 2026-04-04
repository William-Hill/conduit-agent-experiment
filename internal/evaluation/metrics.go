package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// EvalInput collects all pipeline result fields needed to build an Evaluation.
type EvalInput struct {
	RunID               string
	TaskID              string
	IssueNumber         int
	Difficulty          string
	BlastRadius         string
	TriageDecision      string
	ImplementerSuccess  bool
	FilesChanged        int
	DiffLines           int
	VerifierPass        bool
	ArchitectDecision   string
	ArchitectConfidence string
	PRCreated           bool
	PRURL               string
	FailureMode         models.FailureMode
	FailureDetail       string
	TotalDurationMs     int64
	LLMCalls            int
	LLMTokensUsed       int
}

// BuildEvaluation maps EvalInput fields to a models.Evaluation 1:1.
func BuildEvaluation(input EvalInput) models.Evaluation {
	return models.Evaluation{
		RunID:               input.RunID,
		TaskID:              input.TaskID,
		IssueNumber:         input.IssueNumber,
		Difficulty:          input.Difficulty,
		BlastRadius:         input.BlastRadius,
		TriageDecision:      input.TriageDecision,
		ImplementerSuccess:  input.ImplementerSuccess,
		FilesChanged:        input.FilesChanged,
		DiffLines:           input.DiffLines,
		VerifierPass:        input.VerifierPass,
		ArchitectDecision:   input.ArchitectDecision,
		ArchitectConfidence: input.ArchitectConfidence,
		PRCreated:           input.PRCreated,
		PRURL:               input.PRURL,
		FailureMode:         input.FailureMode,
		FailureDetail:       input.FailureDetail,
		TotalDurationMs:     input.TotalDurationMs,
		LLMCalls:            input.LLMCalls,
		LLMTokensUsed:       input.LLMTokensUsed,
	}
}

// WriteEvaluationJSON marshals eval to JSON and writes it to dir/evaluation.json.
func WriteEvaluationJSON(dir string, eval models.Evaluation) error {
	data, err := json.MarshalIndent(eval, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling evaluation JSON: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evaluation.json"), data, 0644); err != nil {
		return fmt.Errorf("writing evaluation.json: %w", err)
	}
	return nil
}
