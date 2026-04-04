package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/evaluation"
	ghub "github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/reporting"
)

var cfgFile string

func main() {
	root := &cobra.Command{
		Use:   "conduit-experiment",
		Short: "Agent-assisted maintenance experiment for Conduit",
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "configs/experiment.yaml", "config file path")

	root.AddCommand(newRunCmd())
	root.AddCommand(newIndexCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newSelectCmd())
	root.AddCommand(newScorecardCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (config.Config, error) {
	return config.Load(cfgFile)
}

func newIndexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "Index the target repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
			if err != nil {
				return fmt.Errorf("walking repo: %w", err)
			}
			fmt.Printf("Indexed %d files in %s\n", len(inv.Files), cfg.Target.RepoPath)

			counts := make(map[ingest.FileCategory]int)
			for _, f := range inv.Files {
				counts[f.Category]++
			}
			for cat, n := range counts {
				fmt.Printf("  %-10s %d\n", cat, n)
			}
			return nil
		},
	}
}

func newRunCmd() *cobra.Command {
	var taskPath string
	var modelsFile string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a task against the target repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			mcfg, err := config.LoadModels(modelsFile)
			if err != nil {
				return fmt.Errorf("loading models config: %w", err)
			}
			if mcfg.APIKey == "" {
				return fmt.Errorf("GEMINI_API_KEY env var is required")
			}

			task, err := loadTask(taskPath)
			if err != nil {
				return fmt.Errorf("loading task: %w", err)
			}

			ghAdapter := newGitHubAdapter(cfg.GitHub)

			fmt.Printf("Running task %s: %s\n", task.ID, task.Title)
			result, err := orchestrator.RunWorkflow(cmd.Context(), task, cfg, mcfg, ghAdapter)
			if err != nil {
				return fmt.Errorf("workflow failed: %w", err)
			}

			fmt.Printf("Triage: %s (%s)\n", result.TriageDecision.Decision, result.TriageDecision.Reason)

			if result.TriageDecision.Decision != agents.DecisionAccept {
				fmt.Printf("Task %s, skipping verification\n", result.TriageDecision.Decision)
			} else {
				fmt.Printf("Verification: %s\n", result.VerifierReport.Summary)
			}

			outDir := filepath.Join(cfg.Reporting.OutputDir, result.Run.ID)
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			if err := reporting.WriteRunJSON(outDir, result.Run); err != nil {
				return fmt.Errorf("writing run JSON: %w", err)
			}
			if err := reporting.WriteDossierJSON(outDir, result.Dossier); err != nil {
				return fmt.Errorf("writing dossier JSON: %w", err)
			}

			if err := evaluation.WriteEvaluationJSON(outDir, result.Evaluation); err != nil {
				return fmt.Errorf("writing evaluation JSON: %w", err)
			}

			md, err := reporting.RenderMarkdown(result.Run, result.Dossier, result.Task)
			if err != nil {
				return fmt.Errorf("rendering markdown: %w", err)
			}
			reportPath := filepath.Join(outDir, "report.md")
			if err := os.WriteFile(reportPath, []byte(md), 0644); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}

			fmt.Printf("\nRun complete: %s\n", result.Run.ID)
			fmt.Printf("Status: %s\n", result.Run.FinalStatus)
			fmt.Printf("Output: %s/\n", outDir)

			if result.PRURL != "" {
				fmt.Printf("PR: %s\n", result.PRURL)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&taskPath, "task", "", "path to task JSON file (required)")
	cmd.Flags().StringVar(&modelsFile, "models", "configs/models.yaml", "models config file path")
	cmd.MarkFlagRequired("task")
	return cmd
}

func newReportCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a report for a completed run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			runDir := filepath.Join(cfg.Reporting.OutputDir, runID)
			reportPath := filepath.Join(runDir, "report.md")
			data, err := os.ReadFile(reportPath)
			if err != nil {
				return fmt.Errorf("reading report: %w", err)
			}
			fmt.Print(string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "run ID to display (required)")
	cmd.MarkFlagRequired("run-id")
	return cmd
}

func newSelectCmd() *cobra.Command {
	var limit int
	var labels []string
	var modelsFile string

	cmd := &cobra.Command{
		Use:   "select",
		Short: "Select and rank GitHub issues as candidate tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			mcfg, err := config.LoadModels(modelsFile)
			if err != nil {
				return fmt.Errorf("loading models config: %w", err)
			}
			if mcfg.APIKey == "" {
				return fmt.Errorf("GEMINI_API_KEY env var is required")
			}

			ghAdapter := newGitHubAdapter(cfg.GitHub)
			if ghAdapter == nil {
				return fmt.Errorf("github.owner and github.repo are required for issue selection")
			}

			opts := ghub.IssueListOpts{
				Limit: 100,
			}
			if len(labels) > 0 {
				opts.Labels = labels
			}

			issues, err := ghAdapter.ListIssues(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("listing issues: %w", err)
			}

			filtered := agents.FilterIssues(issues)

			selectorModel := mcfg.ModelForRole("selector", "gemini-2.5-flash")
			llmClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, selectorModel)

			ranked, _, err := agents.RankIssues(cmd.Context(), llmClient, selectorModel, filtered)
			if err != nil {
				return fmt.Errorf("ranking issues: %w", err)
			}

			if len(ranked) > limit {
				ranked = ranked[:limit]
			}

			// Print summary table.
			fmt.Printf("%-6s %-12s %-12s %s\n", "Issue", "Difficulty", "BlastRadius", "Rationale")
			fmt.Println(strings.Repeat("-", 80))
			for _, r := range ranked {
				rationale := r.Rationale
				if len(rationale) > 50 {
					rationale = rationale[:47] + "..."
				}
				fmt.Printf("#%-5d %-12s %-12s %s\n", r.Number, r.Difficulty, r.BlastRadius, rationale)
			}

			// Build issue lookup map.
			issueMap := make(map[int]ghub.Issue, len(issues))
			for _, iss := range issues {
				issueMap[iss.Number] = iss
			}

			// Write task JSON files.
			tasksDir := "data/tasks"
			if err := os.MkdirAll(tasksDir, 0755); err != nil {
				return fmt.Errorf("creating tasks dir: %w", err)
			}

			for _, r := range ranked {
				iss, ok := issueMap[r.Number]
				if !ok {
					continue
				}
				task := agents.RankedToTask(r, iss)
				data, err := json.MarshalIndent(task, "", "  ")
				if err != nil {
					return fmt.Errorf("marshalling task %d: %w", r.Number, err)
				}
				taskPath := filepath.Join(tasksDir, fmt.Sprintf("task-gh-%d.json", r.Number))
				if err := os.WriteFile(taskPath, data, 0644); err != nil {
					return fmt.Errorf("writing task file %s: %w", taskPath, err)
				}
				fmt.Printf("Wrote %s\n", taskPath)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 5, "maximum number of issues to select")
	cmd.Flags().StringSliceVar(&labels, "labels", nil, "filter issues by labels")
	cmd.Flags().StringVar(&modelsFile, "models", "configs/models.yaml", "models config file path")
	return cmd
}

func newScorecardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scorecard",
		Short: "Generate a scorecard aggregating all run evaluations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			sc, err := evaluation.GenerateScorecard(cfg.Reporting.OutputDir)
			if err != nil {
				return fmt.Errorf("generating scorecard: %w", err)
			}

			fmt.Print(evaluation.FormatScorecard(sc))
			return nil
		},
	}
}

func newGitHubAdapter(cfg config.GitHubConfig) *ghub.Adapter {
	if cfg.Owner == "" || cfg.Repo == "" {
		return nil
	}
	return &ghub.Adapter{
		Owner:      cfg.Owner,
		Repo:       cfg.Repo,
		BaseBranch: cfg.BaseBranch,
		ForkOwner:  cfg.ForkOwner,
	}
}

func loadTask(path string) (models.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return models.Task{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var task models.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return models.Task{}, fmt.Errorf("parsing task JSON: %w", err)
	}
	return task, nil
}
