package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const archivistSystemPrompt = `You are an expert software archivist. Given a maintenance task and a list of files from a repository, your job is to identify the most relevant files, docs, and commands for completing the task.

Respond with a JSON object containing exactly these fields:
- "summary": a concise 1-2 sentence summary of what the task requires
- "relevant_files": an array of the most relevant file paths (up to 20), ranked by relevance
- "relevant_docs": an array of the most relevant doc/ADR paths
- "suggested_commands": an array of commands to validate the work
- "risks": an array of potential risks
- "open_questions": an array of unresolved questions

Respond ONLY with the JSON object, no markdown fences or extra text.`

type archivistResponse struct {
	Summary           string   `json:"summary"`
	RelevantFiles     []string `json:"relevant_files"`
	RelevantDocs      []string `json:"relevant_docs"`
	SuggestedCommands []string `json:"suggested_commands"`
	Risks             []string `json:"risks"`
	OpenQuestions     []string `json:"open_questions"`
}

// EnhanceDossier uses an LLM to improve the keyword-based dossier.
// On LLM failure or bad response, it returns the original dossier unchanged.
func EnhanceDossier(ctx context.Context, client *llm.Client, modelName string, task models.Task, original models.Dossier) (models.Dossier, models.LLMCall, error) {
	userPrompt := buildArchivistPrompt(task, original)

	response, call, err := callLLM(ctx, client, "archivist", modelName, archivistSystemPrompt, userPrompt)
	if err != nil {
		log.Printf("archivist LLM call failed, using keyword dossier: %v", err)
		return original, call, nil
	}

	var parsed archivistResponse
	cleaned := cleanJSONResponse(response)

	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		log.Printf("archivist response not valid JSON, using keyword dossier: %v", err)
		return original, call, nil
	}

	enhanced := models.Dossier{
		TaskID:         original.TaskID,
		Summary:        parsed.Summary,
		RelatedFiles:   parsed.RelevantFiles,
		RelatedDocs:    parsed.RelevantDocs,
		LikelyCommands: parsed.SuggestedCommands,
		Risks:          parsed.Risks,
		OpenQuestions:  parsed.OpenQuestions,
	}

	if enhanced.Summary == "" {
		enhanced.Summary = original.Summary
	}
	if len(enhanced.RelatedFiles) == 0 {
		enhanced.RelatedFiles = original.RelatedFiles
	}
	if len(enhanced.RelatedDocs) == 0 {
		enhanced.RelatedDocs = original.RelatedDocs
	}
	if len(enhanced.LikelyCommands) == 0 {
		enhanced.LikelyCommands = original.LikelyCommands
	}
	if len(enhanced.Risks) == 0 {
		enhanced.Risks = original.Risks
	}
	if len(enhanced.OpenQuestions) == 0 {
		enhanced.OpenQuestions = original.OpenQuestions
	}

	return enhanced, call, nil
}

func buildArchivistPrompt(task models.Task, dossier models.Dossier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n")
	fmt.Fprintf(&b, "ID: %s\n", task.ID)
	fmt.Fprintf(&b, "Title: %s\n", task.Title)
	fmt.Fprintf(&b, "Description: %s\n", task.Description)
	fmt.Fprintf(&b, "Difficulty: %s\n", task.Difficulty)
	fmt.Fprintf(&b, "Blast Radius: %s\n\n", task.BlastRadius)

	if len(task.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n\n", strings.Join(task.Labels, ", "))
	}

	fmt.Fprintf(&b, "## Candidate Files (%d total)\n", len(dossier.RelatedFiles))
	for _, f := range dossier.RelatedFiles {
		fmt.Fprintf(&b, "- %s\n", f)
	}

	fmt.Fprintf(&b, "\n## Candidate Docs (%d total)\n", len(dossier.RelatedDocs))
	for _, d := range dossier.RelatedDocs {
		fmt.Fprintf(&b, "- %s\n", d)
	}

	fmt.Fprintf(&b, "\n## Current Commands\n")
	for _, c := range dossier.LikelyCommands {
		fmt.Fprintf(&b, "- %s\n", c)
	}

	return b.String()
}
