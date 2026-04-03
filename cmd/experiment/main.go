package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/orchestrator"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/reporting"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/retrieval"
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
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a task against the target repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			task, err := loadTask(taskPath)
			if err != nil {
				return fmt.Errorf("loading task: %w", err)
			}

			policy := orchestrator.DefaultPhase1Policy()
			if err := policy.CheckTask(task); err != nil {
				return fmt.Errorf("policy violation: %w", err)
			}

			fmt.Printf("Indexing repository at %s...\n", cfg.Target.RepoPath)
			inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
			if err != nil {
				return fmt.Errorf("walking repo: %w", err)
			}
			fmt.Printf("Indexed %d files\n", len(inv.Files))

			fmt.Println("Building dossier...")
			dossier := retrieval.BuildDossier(task, inv)
			fmt.Printf("Found %d related files, %d related docs\n",
				len(dossier.RelatedFiles), len(dossier.RelatedDocs))

			runID := fmt.Sprintf("run-%s-%s", task.ID, time.Now().Format("20060102-150405"))
			run := models.Run{
				ID:            runID,
				TaskID:        task.ID,
				StartedAt:     time.Now(),
				AgentsInvoked: []string{"archivist"},
				FinalStatus:   models.RunStatusSuccess,
				HumanDecision: models.HumanDecisionPending,
			}

			outDir := filepath.Join(cfg.Reporting.OutputDir, runID)
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			run.EndedAt = time.Now()

			if err := reporting.WriteRunJSON(outDir, run); err != nil {
				return fmt.Errorf("writing run JSON: %w", err)
			}
			if err := reporting.WriteDossierJSON(outDir, dossier); err != nil {
				return fmt.Errorf("writing dossier JSON: %w", err)
			}

			md, err := reporting.RenderMarkdown(run, dossier, task)
			if err != nil {
				return fmt.Errorf("rendering markdown: %w", err)
			}
			reportPath := filepath.Join(outDir, "report.md")
			if err := os.WriteFile(reportPath, []byte(md), 0644); err != nil {
				return fmt.Errorf("writing report: %w", err)
			}

			fmt.Printf("\nRun complete: %s\n", runID)
			fmt.Printf("Output: %s/\n", outDir)
			fmt.Printf("  run.json\n  dossier.json\n  report.md\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&taskPath, "task", "", "path to task JSON file (required)")
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
