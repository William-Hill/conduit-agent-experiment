package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const (
	RecommendApprove = "approve"
	RecommendRevise  = "revise"
	RecommendReject  = "reject"
)

const architectSystemPrompt = `You are a senior software architect reviewing a proposed patch for an open source project. Evaluate the patch for architectural alignment, semantic safety, and reviewability.

Consider:
1. Does the patch stay within the subsystem's boundaries?
2. Does it contradict any ADR guidance?
3. Are there semantic risks (behavior changes, compatibility breaks, concurrency concerns)?
4. Is the diff minimal and reviewable?
5. Does the verification report support confidence in the change?
6. Are the implementer's stated assumptions valid?

Respond with a JSON object containing exactly these fields:
- "recommendation": one of "approve", "revise", "reject"
- "confidence": one of "high", "medium", "low"
- "alignment_notes": brief description of architectural alignment
- "risks_identified": array of identified risks
- "adr_conflicts": array of ADR conflicts found
- "suggestions": array of improvement suggestions
- "rationale": one paragraph explaining the recommendation

Respond ONLY with the JSON object, no markdown fences or extra text.`

// ArchitectInput holds all the information the Architect agent needs to review a patch.
type ArchitectInput struct {
	Diff             string
	Dossier          models.Dossier
	Plan             PatchPlan
	VerifierReport   VerifierReport
	SupplementalDocs map[string]string // path -> content of ADRs/docs
	FailedFiles      []string          // files where generation failed (partial implementation)
}

// ArchitectReviewResult is the structured output from the Architect agent.
type ArchitectReviewResult struct {
	Recommendation  string   `json:"recommendation"` // approve, revise, reject
	Confidence      string   `json:"confidence"`     // high, medium, low
	AlignmentNotes  string   `json:"alignment_notes"`
	RisksIdentified []string `json:"risks_identified"`
	ADRConflicts    []string `json:"adr_conflicts"`
	Suggestions     []string `json:"suggestions"`
	Rationale       string   `json:"rationale"`
}

// ArchitectReview asks the LLM to evaluate a patch for architectural alignment
// and semantic safety. It returns an error if the LLM response cannot be parsed.
func ArchitectReview(ctx context.Context, client *llm.Client, modelName string, input ArchitectInput) (ArchitectReviewResult, models.LLMCall, error) {
	userPrompt := buildArchitectPrompt(input)

	response, call, err := callLLM(ctx, client, "architect", modelName, architectSystemPrompt, userPrompt)
	if err != nil {
		return ArchitectReviewResult{}, call, fmt.Errorf("architect LLM call failed: %w", err)
	}

	cleaned := cleanJSONResponse(response)

	var result ArchitectReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return ArchitectReviewResult{}, call, fmt.Errorf("architect response not valid JSON: %w", err)
	}

	return result, call, nil
}

func buildArchitectPrompt(input ArchitectInput) string {
	var b strings.Builder

	// Patch plan summary, design choices, and assumptions.
	fmt.Fprintf(&b, "## Patch Plan\n")
	fmt.Fprintf(&b, "Summary: %s\n\n", input.Plan.PlanSummary)

	if len(input.Plan.DesignChoices) > 0 {
		fmt.Fprintf(&b, "### Design Choices\n")
		for _, d := range input.Plan.DesignChoices {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(input.Plan.Assumptions) > 0 {
		fmt.Fprintf(&b, "### Assumptions\n")
		for _, a := range input.Plan.Assumptions {
			fmt.Fprintf(&b, "- %s\n", a)
		}
		fmt.Fprintf(&b, "\n")
	}

	// Dossier summary and risks.
	fmt.Fprintf(&b, "## Dossier\n")
	fmt.Fprintf(&b, "Summary: %s\n", input.Dossier.Summary)
	if len(input.Dossier.Risks) > 0 {
		fmt.Fprintf(&b, "Risks:\n")
		for _, r := range input.Dossier.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	fmt.Fprintf(&b, "\n")

	// Verifier report.
	fmt.Fprintf(&b, "## Verification Report\n")
	pass := "FAIL"
	if input.VerifierReport.OverallPass {
		pass = "PASS"
	}
	fmt.Fprintf(&b, "Overall: %s\n", pass)
	fmt.Fprintf(&b, "Summary: %s\n\n", input.VerifierReport.Summary)

	// The diff.
	fmt.Fprintf(&b, "## Diff\n```diff\n%s\n```\n\n", input.Diff)

	// Failed files from partial implementation.
	if len(input.FailedFiles) > 0 {
		fmt.Fprintf(&b, "## Incomplete Files (generation failed)\n")
		fmt.Fprintf(&b, "The following files were planned but could not be generated. The diff is partial.\n")
		for _, f := range input.FailedFiles {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		fmt.Fprintf(&b, "\n")
	}

	// Supplemental docs (ADRs etc).
	if len(input.SupplementalDocs) > 0 {
		fmt.Fprintf(&b, "## Supplemental Documents\n")
		for path, content := range input.SupplementalDocs {
			fmt.Fprintf(&b, "### %s\n%s\n\n", path, content)
		}
	}

	return b.String()
}
