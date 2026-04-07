package archivist

import (
	"context"
	"fmt"
	"path/filepath"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const archivistInstruction = `You are a research archivist for an open source Go project. Given a GitHub issue, explore the repository and identify the files, context, and approach needed to fix it.

## Workflow
1. Read the issue carefully.
2. Use list_dir to understand the top-level repo structure.
3. Use search_files to find code related to the issue.
4. Use read_file to examine the most relevant files.
5. Identify the minimal set of files that need to change.
6. Call save_dossier with your findings.

## Rules
- Include only files directly relevant to fixing the issue (aim for 3-10 files).
- For each file, explain WHY it's relevant.
- Be conservative — fewer well-chosen files are better than many loosely related ones.
- Suggest a concrete, specific approach for the fix.
- Flag any risks (breaking changes, test impacts, API changes).
- Do NOT attempt to fix the issue — just research it.
- You MUST call save_dossier before finishing.`

// NewArchivistAgent creates the ADK Go archivist agent with the given model and tools.
func NewArchivistAgent(m model.LLM, tools []tool.Tool) (agent.Agent, error) {
	a, err := llmagent.New(llmagent.Config{
		Name:        "archivist_agent",
		Description: "Explores a cloned repository and builds a research dossier for a GitHub issue.",
		Instruction: archivistInstruction,
		Model:       m,
		Tools:       tools,
	})
	if err != nil {
		return nil, fmt.Errorf("creating archivist agent: %w", err)
	}
	return a, nil
}

// RunArchivist creates and runs the archivist agent end-to-end.
// It explores the repo at repoDir, writes a dossier to outputDir, and returns it.
func RunArchivist(ctx context.Context, geminiKey, repoDir, outputDir, issueTitle, issueBody string) (*Dossier, error) {
	m, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{
		APIKey: geminiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("creating gemini model: %w", err)
	}

	tools, err := NewTools(repoDir, outputDir)
	if err != nil {
		return nil, fmt.Errorf("creating tools: %w", err)
	}

	a, err := NewArchivistAgent(m, tools)
	if err != nil {
		return nil, fmt.Errorf("creating agent: %w", err)
	}

	ss := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:        "archivist",
		Agent:          a,
		SessionService: ss,
	})
	if err != nil {
		return nil, fmt.Errorf("creating runner: %w", err)
	}

	_, err = ss.Create(ctx, &session.CreateRequest{
		AppName:   "archivist",
		UserID:    "system",
		SessionID: "archivist-run",
	})
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	prompt := fmt.Sprintf("## Issue: %s\n\n%s", issueTitle, issueBody)
	userMsg := genai.NewContentFromText(prompt, "user")

	for event, err := range r.Run(ctx, "system", "archivist-run", userMsg, agent.RunConfig{}) {
		if err != nil {
			return nil, fmt.Errorf("runner event error: %w", err)
		}
		_ = event // The save_dossier tool fires as a side effect during the run.
	}

	dossier, err := LoadDossier(filepath.Join(outputDir, "dossier.json"))
	if err != nil {
		return nil, fmt.Errorf("loading dossier: %w", err)
	}

	return dossier, nil
}
