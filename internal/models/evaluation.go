package models

// FailureMode categorizes why a task run failed.
type FailureMode string

const (
	FailureRetrievalFailure    FailureMode = "retrieval_failure"
	FailureTaskMisclassified   FailureMode = "task_misclassification"
	FailureHallucination       FailureMode = "implementation_hallucination"
	FailureSemanticIncorrect   FailureMode = "semantically_incorrect_fix"
	FailureTestFalseConfidence FailureMode = "test_false_confidence"
	FailureArchitectureDrift   FailureMode = "architecture_drift"
	FailureEnvironment         FailureMode = "environment_setup_failure"
	FailureInsufficientContext FailureMode = "insufficient_repository_context"
	FailureExcessiveIteration  FailureMode = "excessive_iteration_cost"
	FailureHumanRejection      FailureMode = "human_rejection"
)

// Evaluation captures the assessment of a completed run.
type Evaluation struct {
	RunID               string      `json:"run_id"`
	TaskID              string      `json:"task_id"`
	IssueNumber         int         `json:"issue_number,omitempty"`
	Difficulty          string      `json:"difficulty"`
	BlastRadius         string      `json:"blast_radius"`
	TriageDecision      string      `json:"triage_decision"`
	ImplementerSuccess  bool        `json:"implementer_success"`
	FilesChanged        int         `json:"files_changed"`
	DiffLines           int         `json:"diff_lines"`
	VerifierPass        bool        `json:"verifier_pass"`
	ArchitectDecision   string      `json:"architect_decision"`
	ArchitectConfidence string      `json:"architect_confidence"`
	PRCreated           bool        `json:"pr_created"`
	PRURL               string      `json:"pr_url,omitempty"`
	FailureMode         FailureMode `json:"failure_mode,omitempty"`
	FailureDetail       string      `json:"failure_detail,omitempty"`
	TotalDurationMs     int64       `json:"total_duration_ms"`
	LLMCalls            int         `json:"llm_calls"`
	LLMTokensUsed       int         `json:"llm_tokens_used,omitempty"`
	LintPass            bool        `json:"lint_pass"`
	BuildPass           bool        `json:"build_pass"`
	TestsPass           bool        `json:"tests_pass"`
	ReviewScore         int         `json:"review_score"`
	ArchitectureScore   int         `json:"architecture_score"`
	Notes               string      `json:"notes,omitempty"`
}
