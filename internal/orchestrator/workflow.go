package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/retrieval"
)

// WorkflowResult holds all artifacts produced by a single task run.
type WorkflowResult struct {
	Run            models.Run
	Dossier        models.Dossier
	Task           models.Task
	TriageDecision agents.TriageDecision
	VerifierReport agents.VerifierReport
	LLMCalls       []models.LLMCall
}

// RunWorkflow executes the full agent pipeline for a task.
func RunWorkflow(ctx context.Context, task models.Task, cfg config.Config, mcfg config.ModelsConfig) (*WorkflowResult, error) {
	startTime := time.Now()
	runID := fmt.Sprintf("run-%s-%s", task.ID, startTime.Format("20060102-150405"))
	agentsInvoked := []string{}

	// Triage first — reject out-of-policy tasks before any LLM calls or repo ingestion.
	policy := DefaultPhase1Policy()
	triageDecision := agents.Triage(task, models.Dossier{TaskID: task.ID}, policy)
	agentsInvoked = append(agentsInvoked, "triage")

	if triageDecision.Decision == "reject" {
		run := models.Run{
			ID:             runID,
			TaskID:         task.ID,
			StartedAt:      startTime,
			AgentsInvoked:  agentsInvoked,
			TriageDecision: triageDecision.Decision,
			TriageReason:   triageDecision.Reason,
			FinalStatus:    models.RunStatusRejected,
			HumanDecision:  models.HumanDecisionPending,
			EndedAt:        time.Now(),
		}
		return &WorkflowResult{
			Run:            run,
			Dossier:        models.Dossier{TaskID: task.ID},
			Task:           task,
			TriageDecision: triageDecision,
		}, nil
	}

	inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}
	dossier := retrieval.BuildDossier(task, inv)

	var llmCalls []models.LLMCall
	archModel := "gemini-2.5-flash"
	if rc, ok := mcfg.Roles["archivist"]; ok {
		archModel = rc.Model
	}
	llmClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, archModel)
	enhanced, llmCall, err := agents.EnhanceDossier(ctx, llmClient, archModel, task, dossier)
	if err != nil {
		return nil, fmt.Errorf("archivist: %w", err)
	}
	dossier = enhanced
	llmCalls = append(llmCalls, llmCall)
	agentsInvoked = append(agentsInvoked, "archivist")

	run := models.Run{
		ID:             runID,
		TaskID:         task.ID,
		StartedAt:      startTime,
		AgentsInvoked:  agentsInvoked,
		TriageDecision: triageDecision.Decision,
		TriageReason:   triageDecision.Reason,
		LLMCalls:       llmCalls,
		HumanDecision:  models.HumanDecisionPending,
	}

	if triageDecision.Decision == "defer" {
		run.FinalStatus = models.RunStatusFailed
		run.EndedAt = time.Now()
		return &WorkflowResult{
			Run:            run,
			Dossier:        dossier,
			Task:           task,
			TriageDecision: triageDecision,
			LLMCalls:       llmCalls,
		}, nil
	}

	runner := &execution.CommandRunner{
		RepoPath:       cfg.Target.RepoPath,
		UseWorktree:    cfg.Execution.UseWorktree,
		TimeoutSeconds: cfg.Execution.TimeoutSeconds,
	}
	if err := runner.Setup(); err != nil {
		return nil, fmt.Errorf("setting up command runner: %w", err)
	}
	defer runner.Cleanup()

	verifierReport := agents.Verify(ctx, runner, dossier)
	agentsInvoked = append(agentsInvoked, "verifier")

	pass := verifierReport.OverallPass
	run.AgentsInvoked = agentsInvoked
	run.CommandsRun = verifierReport.Commands
	run.VerifierPass = &pass
	run.VerifierSummary = verifierReport.Summary
	run.EndedAt = time.Now()

	if verifierReport.OverallPass {
		run.FinalStatus = models.RunStatusSuccess
	} else {
		run.FinalStatus = models.RunStatusFailed
	}

	return &WorkflowResult{
		Run:            run,
		Dossier:        dossier,
		Task:           task,
		TriageDecision: triageDecision,
		VerifierReport: verifierReport,
		LLMCalls:       llmCalls,
	}, nil
}
