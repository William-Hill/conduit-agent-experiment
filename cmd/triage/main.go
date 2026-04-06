package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/triage"
)

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("GOOGLE_API_KEY environment variable is required")
	}

	owner := envOrDefault("TRIAGE_REPO_OWNER", "ConduitIO")
	repo := envOrDefault("TRIAGE_REPO_NAME", "conduit")
	outputDir := envOrDefault("TRIAGE_OUTPUT_DIR", "data/tasks")

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("creating output directory: %v", err)
	}

	model, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("creating Gemini model: %v", err)
	}

	adapter := &github.Adapter{
		Owner: owner,
		Repo:  repo,
	}

	tools, err := triage.NewTools(adapter, outputDir)
	if err != nil {
		log.Fatalf("creating tools: %v", err)
	}

	triageAgent, err := triage.NewTriageAgent(model, tools)
	if err != nil {
		log.Fatalf("creating triage agent: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(triageAgent),
	}

	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n%s\n", err, l.CommandLineSyntax())
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
