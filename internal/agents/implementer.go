package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const implementerSystemPrompt = `You are an expert software engineer implementing a patch for an open source project. Given a task description, relevant files, and a dossier of context, produce a concrete patch plan.

Respond with a JSON object containing exactly these fields:
- "plan_summary": one paragraph describing what will be changed and why
- "files_to_change": array of objects with "path", "action" (modify/delete), and "description" fields
- "files_to_create": array of objects with "path" and "description" fields
- "design_choices": array of key design decisions made
- "assumptions": array of assumptions made
- "test_recommendations": array of suggested tests

Respond ONLY with the JSON object, no markdown fences or extra text.`

// FileChange describes a file that will be modified or deleted.
type FileChange struct {
	Path        string `json:"path"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

// FileCreate describes a new file that will be created.
type FileCreate struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}

// PatchPlan is the structured output from the Implementer agent describing
// what changes need to be made to fulfill a task.
type PatchPlan struct {
	PlanSummary         string       `json:"plan_summary"`
	FilesToChange       []FileChange `json:"files_to_change"`
	FilesToCreate       []FileCreate `json:"files_to_create"`
	DesignChoices       []string     `json:"design_choices"`
	Assumptions         []string     `json:"assumptions"`
	TestRecommendations []string     `json:"test_recommendations"`
}

// TotalFiles returns the total number of files affected by the plan.
func (p PatchPlan) TotalFiles() int {
	return len(p.FilesToChange) + len(p.FilesToCreate)
}

// CreatePatchPlan asks the LLM to produce a structured patch plan for a task.
func CreatePatchPlan(ctx context.Context, client *llm.Client, modelName string, task models.Task, dossier models.Dossier, fileContents map[string]string) (PatchPlan, []models.LLMCall, error) {
	userPrompt := buildImplementerPrompt(task, dossier, fileContents)

	response, call, err := callLLM(ctx, client, "implementer", modelName, implementerSystemPrompt, userPrompt)
	calls := []models.LLMCall{call}
	if err != nil {
		return PatchPlan{}, calls, fmt.Errorf("implementer LLM call failed: %w", err)
	}

	cleaned := cleanJSONResponse(response)

	var plan PatchPlan
	if err := json.Unmarshal([]byte(cleaned), &plan); err == nil {
		return plan, calls, nil
	} else {
		retryPrompt := fmt.Sprintf(
			"%s\n\n---\nYour previous response was not valid JSON. Parser error: %s\nReturn ONLY a valid JSON object matching the schema. Do not include markdown fences, code blocks, or any text outside the JSON.",
			userPrompt, err.Error(),
		)
		response2, call2, retryErr := callLLM(ctx, client, "implementer-retry", modelName, implementerSystemPrompt, retryPrompt)
		calls = append(calls, call2)
		if retryErr != nil {
			return PatchPlan{}, calls, fmt.Errorf("implementer retry LLM call failed: %w", retryErr)
		}
		cleaned2 := cleanJSONResponse(response2)
		if err2 := json.Unmarshal([]byte(cleaned2), &plan); err2 != nil {
			return PatchPlan{}, calls, fmt.Errorf("implementer response not valid JSON after retry: %w", err2)
		}
		return plan, calls, nil
	}
}

// GenerateFileContent asks the LLM to produce the full content for a single file.
func GenerateFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, packageInventory map[string][]string) (string, models.LLMCall, error) {
	systemPrompt := "You are an expert software engineer. Generate the complete, production-ready file content as requested. Return ONLY the file content — no explanations, no markdown fences."

	userPrompt := buildFileContentPrompt(plan, task, filePath, currentContent, siblingContents, packageInventory)

	response, call, err := callLLM(ctx, client, "implementer", modelName, systemPrompt, userPrompt)
	if err != nil {
		return "", call, fmt.Errorf("implementer file generation LLM call failed: %w", err)
	}

	// Strip markdown fences if present.
	content := response
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.SplitN(trimmed, "\n", 2)
		if len(lines) == 2 {
			content = lines[1]
		} else {
			content = trimmed
		}
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
		content = strings.TrimSpace(content) + "\n"
	}

	return content, call, nil
}

// ReviseFileContent asks the LLM to re-generate file content incorporating
// architect feedback from a prior review round.
func ReviseFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, architectFeedback string, packageInventory map[string][]string) (string, models.LLMCall, error) {
	systemPrompt := "You are an expert software engineer. Revise the file content based on architect feedback. Return ONLY the complete file content — no explanations, no markdown fences."

	userPrompt := buildFileContentPrompt(plan, task, filePath, currentContent, siblingContents, packageInventory)
	userPrompt += fmt.Sprintf("\n\n## Architect Revision Feedback\nThe architect reviewed the previous version and requested revisions:\n\n%s\n\nIncorporate this feedback. Return ONLY the complete revised file content.", architectFeedback)

	response, call, err := callLLM(ctx, client, "implementer-revise", modelName, systemPrompt, userPrompt)
	if err != nil {
		return "", call, fmt.Errorf("implementer revision LLM call failed: %w", err)
	}

	content := response
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.SplitN(trimmed, "\n", 2)
		if len(lines) == 2 {
			content = lines[1]
		} else {
			content = trimmed
		}
		content = strings.TrimSuffix(strings.TrimSpace(content), "```")
		content = strings.TrimSpace(content) + "\n"
	}

	return content, call, nil
}

// ReadFileContents reads the given relative file paths from baseDir and returns
// a map of path -> content. Files that cannot be read are silently skipped.
// Files larger than maxSize bytes are truncated with a marker.
func ReadFileContents(baseDir string, paths []string, maxSize int64) map[string]string {
	result := make(map[string]string)
	for _, p := range paths {
		full := filepath.Join(baseDir, p)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		content := string(data)
		if int64(len(data)) > maxSize {
			content = content[:maxSize] + "\n[... truncated: file exceeds size limit ...]"
		}
		result[p] = content
	}
	return result
}

func buildImplementerPrompt(task models.Task, dossier models.Dossier, fileContents map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n")
	fmt.Fprintf(&b, "ID: %s\n", task.ID)
	fmt.Fprintf(&b, "Title: %s\n", task.Title)
	fmt.Fprintf(&b, "Description: %s\n\n", task.Description)

	fmt.Fprintf(&b, "## Dossier Summary\n%s\n\n", dossier.Summary)

	if len(dossier.Risks) > 0 {
		fmt.Fprintf(&b, "## Risks\n")
		for _, r := range dossier.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(fileContents) > 0 {
		fmt.Fprintf(&b, "## Relevant File Contents\n")
		for path, content := range fileContents {
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", path, content)
		}
	}

	return b.String()
}

func buildFileContentPrompt(plan PatchPlan, task models.Task, filePath, currentContent string, siblingContents map[string]string, packageInventory map[string][]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n%s\n\n", task.Title)
	fmt.Fprintf(&b, "## Plan Summary\n%s\n\n", plan.PlanSummary)

	// Show already-generated sibling files for naming consistency.
	if len(siblingContents) > 0 {
		fmt.Fprintf(&b, "## Already Generated Files in This Plan\n")
		fmt.Fprintf(&b, "IMPORTANT: Use the exact names, types, and signatures defined in these files. Do not invent alternative names.\n\n")
		for path, content := range siblingContents {
			// Cap each sibling at 200 lines to control prompt size.
			lines := strings.Split(content, "\n")
			if len(lines) > 200 {
				content = strings.Join(lines[:200], "\n") + "\n// ... truncated"
			}
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", path, content)
		}
	}

	// Package inventory for import validation.
	if len(packageInventory) > 0 {
		fmt.Fprintf(&b, "## Available Packages and Error Sentinels\n")
		fmt.Fprintf(&b, "IMPORTANT: Only import packages listed below. Do not invent package paths or error constant names.\n\n")
		dirs := make([]string, 0, len(packageInventory))
		for dir := range packageInventory {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			sentinels := packageInventory[dir]
			if len(sentinels) > 0 {
				fmt.Fprintf(&b, "%s: %s\n", dir, strings.Join(sentinels, ", "))
			} else {
				fmt.Fprintf(&b, "%s: (no error sentinels)\n", dir)
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "## File to Generate\n%s\n\n", filePath)

	if currentContent != "" {
		fmt.Fprintf(&b, "## Current Content\n```\n%s\n```\n\n", currentContent)
	}

	fmt.Fprintf(&b, "Generate the complete updated content for %s.", filePath)
	return b.String()
}
