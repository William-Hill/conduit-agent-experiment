package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Summary and DesignChoices/Assumptions are also exposed as convenient
	// aliases used by the Architect agent prompt builder.
	Summary string `json:"-"`
}

// TotalFiles returns the total number of files affected by the plan.
func (p PatchPlan) TotalFiles() int {
	return len(p.FilesToChange) + len(p.FilesToCreate)
}

// CreatePatchPlan asks the LLM to produce a structured patch plan for a task.
func CreatePatchPlan(ctx context.Context, client *llm.Client, modelName string, task models.Task, dossier models.Dossier, fileContents map[string]string) (PatchPlan, models.LLMCall, error) {
	userPrompt := buildImplementerPrompt(task, dossier, fileContents)

	start := time.Now()
	response, err := client.Complete(ctx, implementerSystemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "implementer",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	if err != nil {
		return PatchPlan{}, call, fmt.Errorf("implementer LLM call failed: %w", err)
	}

	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var plan PatchPlan
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return PatchPlan{}, call, fmt.Errorf("implementer response not valid JSON: %w", err)
	}

	// Populate the convenience Summary alias.
	plan.Summary = plan.PlanSummary

	return plan, call, nil
}

// GenerateFileContent asks the LLM to produce the full content for a single file.
func GenerateFileContent(ctx context.Context, client *llm.Client, modelName string, plan PatchPlan, task models.Task, filePath, currentContent string) (string, models.LLMCall, error) {
	systemPrompt := "You are an expert software engineer. Generate the complete, production-ready file content as requested. Return ONLY the file content — no explanations, no markdown fences."

	userPrompt := buildFileContentPrompt(plan, task, filePath, currentContent)

	start := time.Now()
	response, err := client.Complete(ctx, systemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    "implementer",
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

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

// ReadFileContents reads the given relative file paths from baseDir and returns
// a map of path -> content. Files that cannot be read are silently skipped.
// Files larger than maxSize bytes are truncated with a marker.
func ReadFileContents(baseDir string, paths []string, maxSize int64) map[string]string {
	result := make(map[string]string)
	for _, p := range paths {
		full := filepath.Join(baseDir, p)
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		content := string(data)
		if info.Size() > maxSize {
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

func buildFileContentPrompt(plan PatchPlan, task models.Task, filePath, currentContent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Task\n%s\n\n", task.Title)
	fmt.Fprintf(&b, "## Plan Summary\n%s\n\n", plan.PlanSummary)
	fmt.Fprintf(&b, "## File to Generate\n%s\n\n", filePath)

	if currentContent != "" {
		fmt.Fprintf(&b, "## Current Content\n```\n%s\n```\n\n", currentContent)
	}

	fmt.Fprintf(&b, "Generate the complete updated content for %s.", filePath)
	return b.String()
}
