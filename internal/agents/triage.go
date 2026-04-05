package agents

import (
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const (
	DecisionAccept = "accept"
	DecisionReject = "reject"
	DecisionDefer  = "defer"
)

// PolicyChecker is satisfied by any type that can validate a task against a policy.
type PolicyChecker interface {
	CheckTask(models.Task) error
}

// TriageDecision records the triage outcome for a task.
type TriageDecision struct {
	Decision string `json:"decision"` // "accept", "reject", "defer"
	Reason   string `json:"reason"`
}

// Triage evaluates whether a task should proceed based on policy and dossier quality.
func Triage(task models.Task, dossier models.Dossier, policy PolicyChecker) TriageDecision {
	if err := policy.CheckTask(task); err != nil {
		return TriageDecision{
			Decision: DecisionReject,
			Reason:   err.Error(),
		}
	}

	if len(dossier.RelatedFiles) == 0 && len(dossier.RelatedDocs) == 0 && len(dossier.OpenQuestions) > 0 {
		return TriageDecision{
			Decision: DecisionDefer,
			Reason:   "no related files found and open questions remain",
		}
	}

	return TriageDecision{
		Decision: DecisionAccept,
		Reason:   "task within policy limits and dossier has relevant context",
	}
}
