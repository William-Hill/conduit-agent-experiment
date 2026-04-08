package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/agents"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/config"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/evaluation"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/execution"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/retrieval"
)

// WorkflowResult holds all artifacts produced by a single task run.
type WorkflowResult struct {
	Run             models.Run
	Dossier         models.Dossier
	Task            models.Task
	TriageDecision  agents.TriageDecision
	PatchPlan       agents.PatchPlan
	VerifierReport  agents.VerifierReport
	ArchitectReview agents.ArchitectReviewResult
	Evaluation      models.Evaluation
	LLMCalls        []models.LLMCall
	PRURL           string
	Budget          cost.Budget
}

// RunWorkflow executes the full agent pipeline for a task.
func RunWorkflow(ctx context.Context, task models.Task, cfg config.Config, mcfg config.ModelsConfig, ghAdapter *github.Adapter) (*WorkflowResult, error) {
	startTime := time.Now()
	runID := fmt.Sprintf("run-%s-%s", task.ID, startTime.Format("20060102-150405"))
	agentsInvoked := []string{}

	// --- 1. Triage: reject out-of-policy tasks before any LLM calls or repo ingestion ---
	policy := DefaultPhase1Policy()
	if cfg.Policy.MaxFilesChanged > 0 {
		policy.MaxFilesChanged = cfg.Policy.MaxFilesChanged
	}
	if cfg.Policy.AllowPush {
		policy.AllowPush = cfg.Policy.AllowPush
	}

	triageDecision := agents.Triage(task, models.Dossier{TaskID: task.ID}, policy)
	agentsInvoked = append(agentsInvoked, "triage")

	if triageDecision.Decision == agents.DecisionReject {
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

	budget := cost.LoadBudget()

	// --- 2. Repo walk + keyword dossier ---
	inv, err := ingest.WalkRepo(cfg.Target.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}
	dossier := retrieval.BuildDossier(task, inv)

	// --- 3. Archivist: enhance dossier via LLM ---
	var llmCalls []models.LLMCall
	archModel := mcfg.ModelForRole("archivist", "gemini-2.5-flash")
	llmClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, archModel)
	enhanced, archCalls, err := agents.EnhanceDossier(ctx, llmClient, archModel, task, dossier)
	llmCalls = append(llmCalls, archCalls...)
	if err != nil {
		return nil, fmt.Errorf("archivist: %w", err)
	}
	dossier = enhanced
	agentsInvoked = append(agentsInvoked, "archivist")

	if err := budget.CheckStep("archivist", llmCalls); err != nil {
		run := models.Run{
			ID: runID, TaskID: task.ID, StartedAt: startTime,
			AgentsInvoked: agentsInvoked, TriageDecision: triageDecision.Decision,
			TriageReason: err.Error(), FinalStatus: models.RunStatusFailed,
			HumanDecision: models.HumanDecisionPending, LLMCalls: llmCalls,
			EndedAt: time.Now(),
		}
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, LLMCalls: llmCalls, Budget: budget}, nil
	}
	if err := budget.CheckTotal(llmCalls); err != nil {
		run := models.Run{
			ID: runID, TaskID: task.ID, StartedAt: startTime,
			AgentsInvoked: agentsInvoked, TriageDecision: triageDecision.Decision,
			TriageReason: err.Error(), FinalStatus: models.RunStatusFailed,
			HumanDecision: models.HumanDecisionPending, LLMCalls: llmCalls,
			EndedAt: time.Now(),
		}
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, LLMCalls: llmCalls, Budget: budget}, nil
	}

	// --- 4. Triage re-check after archivist ---
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

	if triageDecision.Decision == agents.DecisionDefer {
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

	// --- 5. Setup worktree ---
	runner := &execution.CommandRunner{
		RepoPath:       cfg.Target.RepoPath,
		UseWorktree:    cfg.Execution.UseWorktree,
		TimeoutSeconds: cfg.Execution.TimeoutSeconds,
	}
	if err := runner.Setup(); err != nil {
		return nil, fmt.Errorf("setting up command runner: %w", err)
	}
	defer runner.Cleanup()

	// --- 6. Implementer Phase 1: read top files and create patch plan ---
	implModel := mcfg.ModelForRole("implementer", archModel)
	implClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, implModel)

	topN := len(dossier.RelatedFiles)
	if topN > 10 {
		topN = 10
	}
	topFiles := dossier.RelatedFiles[:topN]
	fileContents := agents.ReadFileContents(runner.WorkDir, topFiles, 32*1024)

	plan, planCall, err := agents.CreatePatchPlan(ctx, implClient, implModel, task, dossier, fileContents)
	if err != nil {
		return nil, fmt.Errorf("implementer plan: %w", err)
	}
	llmCalls = append(llmCalls, planCall)
	agentsInvoked = append(agentsInvoked, "implementer")

	if err := budget.CheckStep("implementer", llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		run.ImplementerPlan = plan.PlanSummary
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, LLMCalls: llmCalls, Budget: budget}, nil
	}
	if err := budget.CheckTotal(llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		run.ImplementerPlan = plan.PlanSummary
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, LLMCalls: llmCalls, Budget: budget}, nil
	}

	if err := policy.CheckPatchBreadth(plan.TotalFiles()); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusRejected
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		run.ImplementerPlan = plan.PlanSummary
		return &WorkflowResult{
			Run:            run,
			Dossier:        dossier,
			Task:           task,
			TriageDecision: triageDecision,
			PatchPlan:      plan,
			LLMCalls:       llmCalls,
		}, nil
	}

	// --- 7. Implementer Phase 2: generate file contents ---
	var failedFiles []string
	totalFiles := plan.TotalFiles()

	for _, fc := range plan.FilesToChange {
		fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
		if pathErr != nil {
			log.Printf("skipping file %s: %v", fc.Path, pathErr)
			failedFiles = append(failedFiles, fc.Path)
			continue
		}

		if fc.Action == "delete" {
			if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
				log.Printf("failed to delete %s: %v", fc.Path, err)
				failedFiles = append(failedFiles, fc.Path)
			}
			continue
		}

		currentContent := ""
		if cached, ok := fileContents[fc.Path]; ok {
			currentContent = cached
		} else {
			data, readErr := os.ReadFile(fullPath)
			if readErr == nil {
				currentContent = string(data)
			}
		}

		newContent, genCall, err := agents.GenerateFileContent(ctx, implClient, implModel, plan, task, fc.Path, currentContent)
		if err != nil {
			log.Printf("implementer: generation failed for %s: %v — marking as failed, continuing", fc.Path, err)
			failedFiles = append(failedFiles, fc.Path)
			continue
		}
		llmCalls = append(llmCalls, genCall)

		// Ensure parent directory exists and write file.
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("creating dir for %s: %w", fc.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", fc.Path, err)
		}
	}

	for _, fc := range plan.FilesToCreate {
		fullPath, pathErr := safePath(runner.WorkDir, fc.Path)
		if pathErr != nil {
			log.Printf("skipping file %s: %v", fc.Path, pathErr)
			failedFiles = append(failedFiles, fc.Path)
			continue
		}

		newContent, genCall, err := agents.GenerateFileContent(ctx, implClient, implModel, plan, task, fc.Path, "")
		if err != nil {
			log.Printf("implementer: generation failed for %s: %v — marking as failed, continuing", fc.Path, err)
			failedFiles = append(failedFiles, fc.Path)
			continue
		}
		llmCalls = append(llmCalls, genCall)

		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return nil, fmt.Errorf("creating dir for %s: %w", fc.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", fc.Path, err)
		}
	}

	// Re-check implementer budget after generation calls.
	if err := budget.CheckStep("implementer", llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		run.ImplementerPlan = plan.PlanSummary
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, LLMCalls: llmCalls, Budget: budget}, nil
	}
	if err := budget.CheckTotal(llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		run.ImplementerPlan = plan.PlanSummary
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, LLMCalls: llmCalls, Budget: budget}, nil
	}

	// If all files failed, mark the run as failed with implementation_hallucination.
	if totalFiles > 0 && len(failedFiles) == totalFiles {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.ImplementerPlan = plan.PlanSummary
		run.LLMCalls = llmCalls
		run.EndedAt = time.Now()

		eval := evaluation.BuildEvaluation(evaluation.EvalInput{
			RunID:          runID,
			TaskID:         task.ID,
			IssueNumber:    task.IssueNumber,
			Difficulty:     string(task.Difficulty),
			BlastRadius:    string(task.BlastRadius),
			TriageDecision: triageDecision.Decision,
			FilesChanged:   plan.TotalFiles(),
			FailureMode:    models.FailureHallucination,
			FailureDetail:  fmt.Sprintf("all %d file(s) failed generation: %s", totalFiles, strings.Join(failedFiles, ", ")),
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			LLMCalls:        len(llmCalls),
			LLMTokensUsed:   cost.TotalTokens(llmCalls),
		})

		return &WorkflowResult{
			Run:            run,
			Dossier:        dossier,
			Task:           task,
			TriageDecision: triageDecision,
			PatchPlan:      plan,
			Evaluation:     eval,
			LLMCalls:       llmCalls,
		}, nil
	}

	// --- 8. Git diff ---
	diffLog := runner.Run(ctx, "git diff")
	diff := diffLog.Stdout

	// --- 9. Verifier ---
	verifierReport := agents.Verify(ctx, runner, dossier)
	agentsInvoked = append(agentsInvoked, "verifier")

	// --- 10. Architect ---
	archReviewModel := mcfg.ModelForRole("architect", archModel)
	archReviewClient := llm.NewClient(mcfg.Provider.BaseURL, mcfg.APIKey, archReviewModel)

	supplementalDocs := findSupplementalDocs(runner.WorkDir, dossier)

	architectInput := agents.ArchitectInput{
		Diff:             diff,
		Dossier:          dossier,
		Plan:             plan,
		VerifierReport:   verifierReport,
		SupplementalDocs: supplementalDocs,
		FailedFiles:      failedFiles,
	}

	architectReview, archCalls, err := agents.ArchitectReview(ctx, archReviewClient, archReviewModel, architectInput)
	llmCalls = append(llmCalls, archCalls...)
	if err != nil {
		return nil, fmt.Errorf("architect review: %w", err)
	}
	agentsInvoked = append(agentsInvoked, "architect")

	if err := budget.CheckStep("architect", llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, ArchitectReview: architectReview, LLMCalls: llmCalls, Budget: budget}, nil
	}
	if err := budget.CheckTotal(llmCalls); err != nil {
		run.AgentsInvoked = agentsInvoked
		run.FinalStatus = models.RunStatusFailed
		run.TriageReason = err.Error()
		run.EndedAt = time.Now()
		run.LLMCalls = llmCalls
		return &WorkflowResult{Run: run, Dossier: dossier, Task: task, TriageDecision: triageDecision, PatchPlan: plan, ArchitectReview: architectReview, LLMCalls: llmCalls, Budget: budget}, nil
	}

	// --- 11. GitHub PR ---
	prURL := ""
	if ghAdapter != nil && policy.AllowPush && (architectReview.Recommendation == agents.RecommendApprove) {
		branchName := buildBranchName(task)

		commitMsg := fmt.Sprintf("agent: %s\n\n%s", task.Title, plan.PlanSummary)
		if err := ghAdapter.CreateBranchAndPush(ctx, runner.WorkDir, branchName, commitMsg); err != nil {
			return nil, fmt.Errorf("creating branch and pushing: %w", err)
		}

		prBody := buildPRBody(dossier, plan, verifierReport, architectReview)
		baseBranch := ghAdapter.BaseBranch
		if baseBranch == "" {
			baseBranch = "main"
		}

		url, err := ghAdapter.CreateDraftPR(ctx, github.DraftPRInput{
			Title: task.Title,
			Body:  prBody,
			Head:  branchName,
			Base:  baseBranch,
		})
		if err != nil {
			return nil, fmt.Errorf("creating draft PR: %w", err)
		}
		prURL = url
	}

	// --- 12. Build evaluation ---
	pass := verifierReport.OverallPass
	diffLines := countDiffLines(diff)

	eval := evaluation.BuildEvaluation(evaluation.EvalInput{
		RunID:               runID,
		TaskID:              task.ID,
		IssueNumber:         task.IssueNumber,
		Difficulty:          string(task.Difficulty),
		BlastRadius:         string(task.BlastRadius),
		TriageDecision:      triageDecision.Decision,
		ImplementerSuccess:  diff != "",
		FilesChanged:        plan.TotalFiles(),
		DiffLines:           diffLines,
		VerifierPass:        pass,
		ArchitectDecision:   architectReview.Recommendation,
		ArchitectConfidence: architectReview.Confidence,
		PRCreated:           prURL != "",
		PRURL:               prURL,
		TotalDurationMs:     time.Since(startTime).Milliseconds(),
		LLMCalls:            len(llmCalls),
		LLMTokensUsed:       cost.TotalTokens(llmCalls),
	})

	// --- 13. Finalize run ---
	run.AgentsInvoked = agentsInvoked
	run.CommandsRun = verifierReport.Commands
	run.VerifierPass = &pass
	run.VerifierSummary = verifierReport.Summary
	run.ImplementerPlan = plan.PlanSummary
	run.ImplementerDiff = diff
	run.ArchitectDecision = architectReview.Recommendation
	run.ArchitectReview = architectReview.Rationale
	run.PRURL = prURL
	run.LLMCalls = llmCalls
	run.EndedAt = time.Now()

	if architectReview.Recommendation == agents.RecommendReject || architectReview.Recommendation == agents.RecommendRevise {
		run.FinalStatus = models.RunStatusFailed
	} else if pass {
		run.FinalStatus = models.RunStatusSuccess
	} else {
		run.FinalStatus = models.RunStatusFailed
	}

	return &WorkflowResult{
		Run:             run,
		Dossier:         dossier,
		Task:            task,
		TriageDecision:  triageDecision,
		PatchPlan:       plan,
		VerifierReport:  verifierReport,
		ArchitectReview: architectReview,
		Evaluation:      eval,
		LLMCalls:        llmCalls,
		PRURL:           prURL,
		Budget:          budget,
	}, nil
}

// findSupplementalDocs looks for ADR/design docs in the repo that relate to
// the packages of changed files and returns their content.
func findSupplementalDocs(workDir string, dossier models.Dossier) map[string]string {
	docs := make(map[string]string)

	// Collect doc paths from the dossier.
	for _, docPath := range dossier.RelatedDocs {
		if isADROrDesignDoc(docPath) {
			fullPath := filepath.Join(workDir, docPath)
			data, err := os.ReadFile(fullPath)
			if err == nil {
				docs[docPath] = string(data)
			}
		}
	}

	return docs
}

// isADROrDesignDoc returns true if the path matches ADR or design doc patterns.
func isADROrDesignDoc(path string) bool {
	return strings.Contains(path, "docs/adr/") ||
		strings.Contains(path, "docs/adr\\") ||
		strings.Contains(path, "docs/design-doc") ||
		strings.Contains(path, "docs/design-documents/")
}

// buildBranchName creates a branch name for the agent's PR.
func buildBranchName(task models.Task) string {
	suffix := task.ID
	if task.IssueNumber > 0 {
		suffix = fmt.Sprintf("%d", task.IssueNumber)
	}
	slug := slugify(task.Title)
	return fmt.Sprintf("agent/task-%s-%s", suffix, slug)
}

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a title into a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugifyRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// buildPRBody creates the markdown body for a draft PR.
func buildPRBody(dossier models.Dossier, plan agents.PatchPlan, verifier agents.VerifierReport, architect agents.ArchitectReviewResult) string {
	var b strings.Builder

	b.WriteString("## Dossier Summary\n\n")
	b.WriteString(dossier.Summary)
	b.WriteString("\n\n")

	b.WriteString("## Patch Plan\n\n")
	b.WriteString(plan.PlanSummary)
	b.WriteString("\n\n")

	if len(plan.FilesToChange) > 0 {
		b.WriteString("### Files Changed\n")
		for _, f := range plan.FilesToChange {
			fmt.Fprintf(&b, "- `%s` (%s): %s\n", f.Path, f.Action, f.Description)
		}
		b.WriteString("\n")
	}

	if len(plan.FilesToCreate) > 0 {
		b.WriteString("### Files Created\n")
		for _, f := range plan.FilesToCreate {
			fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Description)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Verifier Results\n\n")
	if verifier.OverallPass {
		b.WriteString("**Status:** PASS\n\n")
	} else {
		b.WriteString("**Status:** FAIL\n\n")
	}
	b.WriteString(verifier.Summary)
	b.WriteString("\n\n")

	b.WriteString("## Architect Review\n\n")
	fmt.Fprintf(&b, "**Recommendation:** %s (confidence: %s)\n\n", architect.Recommendation, architect.Confidence)
	b.WriteString(architect.Rationale)
	b.WriteString("\n\n")

	if len(architect.Suggestions) > 0 {
		b.WriteString("### Suggestions\n")
		for _, s := range architect.Suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n*Generated by conduit-agent-experiment*\n")

	return b.String()
}

// safePath validates that a relative path resolves under the base directory.
func safePath(base, rel string) (string, error) {
	full := filepath.Join(base, filepath.Clean(rel))
	absBase, _ := filepath.Abs(base)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absBase+string(filepath.Separator)) && absFull != absBase {
		return "", fmt.Errorf("path %q escapes worktree root", rel)
	}
	return full, nil
}

// countDiffLines counts the number of lines in a diff string.
func countDiffLines(diff string) int {
	if diff == "" {
		return 0
	}
	return len(strings.Split(diff, "\n"))
}
