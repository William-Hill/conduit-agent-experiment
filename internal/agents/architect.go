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
	NewFiles         map[string]string // path -> content of newly created files (not in git diff)
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
// and semantic safety. If the first response is not valid JSON, it retries once
// with the parse error fed back to the model so it can self-correct. It returns
// all LLM calls made (1 or 2) so token/latency accounting is complete.
func ArchitectReview(ctx context.Context, client *llm.Client, modelName string, input ArchitectInput) (ArchitectReviewResult, []models.LLMCall, error) {
	userPrompt := buildArchitectPrompt(input)

	response, call, err := callLLM(ctx, client, "architect", modelName, architectSystemPrompt, userPrompt)
	calls := []models.LLMCall{call}
	if err != nil {
		return ArchitectReviewResult{}, calls, fmt.Errorf("architect LLM call failed: %w", err)
	}

	cleaned := cleanJSONResponse(response)

	var result ArchitectReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err == nil {
		return result, calls, nil
	} else {
		// Retry once, feeding the parse error back so the model can self-correct.
		retryPrompt := fmt.Sprintf(
			"%s\n\n---\nYour previous response was not valid JSON. Parser error: %s\nReturn ONLY a valid JSON object matching the schema. Do not include markdown fences, code blocks, or any text outside the JSON. Ensure every backslash inside a string is either part of a valid escape (\\\", \\\\, \\/, \\b, \\f, \\n, \\r, \\t, \\uXXXX) or doubled.",
			userPrompt, err.Error(),
		)
		response2, call2, retryErr := callLLM(ctx, client, "architect-retry", modelName, architectSystemPrompt, retryPrompt)
		calls = append(calls, call2)
		if retryErr != nil {
			return ArchitectReviewResult{}, calls, fmt.Errorf("architect retry LLM call failed: %w", retryErr)
		}
		cleaned2 := cleanJSONResponse(response2)
		if err2 := json.Unmarshal([]byte(cleaned2), &result); err2 != nil {
			return ArchitectReviewResult{}, calls, fmt.Errorf("architect response not valid JSON after retry: %w", err2)
		}
		return result, calls, nil
	}
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

	// New files not in the diff (untracked).
	if len(input.NewFiles) > 0 {
		fmt.Fprintf(&b, "## Newly Created Files\n")
		fmt.Fprintf(&b, "These files were created by the implementer but are not in the diff (untracked by git).\n\n")
		for path, content := range input.NewFiles {
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", path, content)
		}
	}

	return b.String()
}
