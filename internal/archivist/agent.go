package archivist

import (
	"context"
	"fmt"
	"log"
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

const archivistInstruction = `You are a research archivist. Given a GitHub issue, find the relevant source files.

You have EXACTLY 3 tools: search_files, read_file, save_dossier.

YOUR TASK IN EXACTLY 5 STEPS:
1. search_files to find relevant code (1 call)
2. read_file on the 2-3 most relevant results (2-3 calls)
3. save_dossier with your findings (1 call)

CRITICAL: You MUST call save_dossier on your 4th or 5th tool call. If you do not call save_dossier, ALL your work is lost and the pipeline fails.

When calling save_dossier, provide:
- summary: one paragraph about what the issue is and what code is involved
- relevant_files: list of {path, reason} for 3-8 files relevant to the fix
- risks: any concerns about the change
- approach: concrete steps to fix the issue`

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
		// Log tool calls for progress visibility
		if event != nil && event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.FunctionCall != nil {
					log.Printf("  [archivist] tool: %s", part.FunctionCall.Name)
				}
			}
		}
	}

	dossier, err := LoadDossier(filepath.Join(outputDir, "dossier.json"))
	if err != nil {
		return nil, fmt.Errorf("loading dossier: %w", err)
	}

	return dossier, nil
}
