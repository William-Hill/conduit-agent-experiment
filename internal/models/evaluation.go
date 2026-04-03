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
	RunID             string      `json:"run_id"`
	LintPass          bool        `json:"lint_pass"`
	BuildPass         bool        `json:"build_pass"`
	TestsPass         bool        `json:"tests_pass"`
	ReviewScore       int         `json:"review_score"`       // 1-5
	ArchitectureScore int         `json:"architecture_score"` // 1-5
	Notes             string      `json:"notes,omitempty"`
	FailureMode       FailureMode `json:"failure_mode,omitempty"`
}
