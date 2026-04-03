package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
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

			fmt.Printf("Running task %s: %s\n", task.ID, task.Title)
			result, err := orchestrator.RunWorkflow(cmd.Context(), task, cfg, mcfg)
			if err != nil {
				return fmt.Errorf("workflow failed: %w", err)
			}

			fmt.Printf("Triage: %s (%s)\n", result.TriageDecision.Decision, result.TriageDecision.Reason)

			if result.TriageDecision.Decision != "accept" {
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
