package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/codereviewer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/implementer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/responder"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/triage"
)

func main() {
	ctx := context.Background()

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	geminiKey := os.Getenv("GOOGLE_API_KEY")
	if geminiKey == "" {
		geminiKey = os.Getenv("GEMINI_API_KEY")
	}
	if geminiKey == "" {
		log.Fatal("GOOGLE_API_KEY or GEMINI_API_KEY environment variable is required")
	}

	owner := envOrDefault("IMPL_REPO_OWNER", "ConduitIO")
	repo := envOrDefault("IMPL_REPO_NAME", "conduit")
	forkOwner := envOrDefault("IMPL_FORK_OWNER", "William-Hill")
	triageDir := envOrDefault("IMPL_TRIAGE_DIR", "data/tasks")
	modelName := os.Getenv("IMPL_MODEL") // empty = Haiku 4.5 default
	maxIter := 15

	// 1. Read triage output, pick issue (override with IMPL_ISSUE_NUMBER)
	var (
		issue *triage.RankedIssue
		err   error
	)
	if numStr := os.Getenv("IMPL_ISSUE_NUMBER"); numStr != "" {
		num, parseErr := strconv.Atoi(numStr)
		if parseErr != nil {
			log.Fatalf("invalid IMPL_ISSUE_NUMBER %q: %v", numStr, parseErr)
		}
		issue, err = findIssueByNumber(triageDir, num)
	} else {
		issue, err = readTopRankedIssue(triageDir)
	}
	if err != nil {
		log.Fatalf("reading triage output: %v", err)
	}
	log.Printf("Selected issue #%d: %s (score %d)", issue.Number, issue.Title, issue.Score)

	// 2. Set up GitHub adapter and HITL config
	adapter := &github.Adapter{
		Owner:      owner,
		Repo:       repo,
		ForkOwner:  forkOwner,
		BaseBranch: "main",
	}
	hitlCfg := hitl.LoadConfig()
	hitlAdapter := &github.HITLAdapter{Adapter: adapter}

	// 3. Gate 1: Issue selection approval (HITL)
	if hitlCfg.Gate1Enabled {
		log.Printf("[HITL] Gate 1 active — requesting approval for issue #%d", issue.Number)

		if err := hitlAdapter.AddLabel(ctx, issue.Number, hitl.LabelCandidate); err != nil {
			log.Printf("[HITL] Warning: failed to apply candidate label: %v", err)
		}

		if err := hitl.PostTriageRationale(ctx, hitlAdapter, issue.Number, issue.Difficulty, issue.BlastRadius, issue.Score, issue.Rationale); err != nil {
			log.Printf("[HITL] Warning: failed to post rationale: %v", err)
		}

		log.Printf("[HITL] Waiting for %s or %s label on issue #%d (polling every %v)...",
			hitl.LabelApproved, hitl.LabelRejected, issue.Number, hitlCfg.Gate1PollInterval)

		label, err := hitl.WaitForLabel(ctx, hitlAdapter, issue.Number,
			[]string{hitl.LabelApproved, hitl.LabelRejected}, hitlCfg.Gate1PollInterval)
		if err != nil {
			log.Fatalf("[HITL] Gate 1 error: %v", err)
		}

		if label == hitl.LabelRejected {
			log.Printf("[HITL] Issue #%d rejected by human, exiting", issue.Number)
			os.Exit(0)
		}
		log.Printf("[HITL] Issue #%d approved by human, proceeding", issue.Number)
	} else {
		log.Printf("[HITL] Gate 1 disabled (mode=%s), proceeding automatically", hitlCfg.Mode)
	}
	fullIssue, err := adapter.GetIssue(ctx, issue.Number)
	if err != nil {
		log.Fatalf("fetching issue #%d: %v", issue.Number, err)
	}

	// 4. Clone repo to temp dir
	repoDir, err := cloneRepo(ctx, owner, repo)
	if err != nil {
		log.Fatalf("cloning repo: %v", err)
	}
	defer os.RemoveAll(repoDir)
	log.Printf("Cloned %s/%s to %s", owner, repo, repoDir)

	// 5. Run archivist (Gemini Flash — cheap exploration)
	log.Printf("Running archivist agent...")
	dossierDir, err := os.MkdirTemp("", "dossier-*")
	if err != nil {
		log.Fatalf("creating dossier dir: %v", err)
	}
	defer os.RemoveAll(dossierDir)

	dossier, err := archivist.RunArchivist(ctx, geminiKey, repoDir, dossierDir, fullIssue.Title, fullIssue.Body)
	if err != nil {
		log.Fatalf("archivist failed: %v", err)
	}
	log.Printf("Archivist found %d relevant files", len(dossier.Files))
	log.Printf("Approach: %s", dossier.Approach)

	// 6. Plan implementation (Gemini Flash — writes exact code changes)
	log.Printf("Running planner agent...")
	plan, err := planner.CreatePlan(ctx, geminiKey, fullIssue.Title, fullIssue.Body, dossier)
	if err != nil {
		log.Fatalf("planner failed: %v", err)
	}
	// Show first 200 chars of plan
	preview := plan.Markdown
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	log.Printf("Plan produced (%d chars): %s", len(plan.Markdown), preview)

	// 7. Review plan (Gemini Flash — validates the plan)
	log.Printf("Running reviewer agent...")
	review, err := planner.ReviewPlan(ctx, geminiKey, fullIssue.Title, fullIssue.Body, dossier, plan)
	if err != nil {
		log.Fatalf("reviewer failed: %v", err)
	}
	if !review.Approved {
		log.Printf("Reviewer feedback: %s", review.Feedback)
		log.Printf("Retrying planner with feedback...")
		plan, err = planner.CreatePlan(ctx, geminiKey, fullIssue.Title, fullIssue.Body+"\n\n## Reviewer Feedback\n"+review.Feedback, dossier)
		if err != nil {
			log.Fatalf("planner retry failed: %v", err)
		}
		log.Printf("Revised plan produced (%d chars)", len(plan.Markdown))

		// Re-review the revised plan
		review, err = planner.ReviewPlan(ctx, geminiKey, fullIssue.Title, fullIssue.Body, dossier, plan)
		if err != nil {
			log.Fatalf("reviewer retry failed: %v", err)
		}
		if !review.Approved {
			log.Fatalf("Revised plan still not approved after retry: %s", review.Feedback)
		}
	}
	log.Printf("Plan approved")

	// 8. Run implementer agent
	log.Printf("Running implementer agent (max %d iterations)...", maxIter)
	implMaxCost := cost.EnvFloat("IMPL_MAX_COST")
	result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxIter, implMaxCost)
	if err != nil {
		log.Fatalf("agent failed: %v", err)
	}
	log.Printf("Agent completed in %d iterations", result.Iterations)
	log.Printf("Summary: %s", result.Summary)

	// Write run artifacts for CI (GitHub Actions artifact upload)
	artifactDir := os.Getenv("IMPL_ARTIFACT_DIR")
	if artifactDir != "" {
		writeRunArtifacts(artifactDir, issue, result, modelName, plan)
	}

	if result.BudgetExceeded {
		log.Printf("Implementer budget exceeded (IMPL_MAX_COST=$%.4f) — halting before PR creation", implMaxCost)
		os.RemoveAll(repoDir)
		os.RemoveAll(dossierDir)
		os.Exit(1)
	}

	// 9. Check for changes (staged, unstaged, and untracked)
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--stat")
	diffCmd.Dir = repoDir
	diffOutput, err := diffCmd.Output()
	if err != nil {
		log.Fatalf("git diff failed: %v", err)
	}

	// Also check for untracked files (git diff misses new files)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOutput, err := statusCmd.Output()
	if err != nil {
		log.Fatalf("git status failed: %v", err)
	}

	if len(diffOutput) == 0 && len(statusOutput) == 0 {
		log.Fatal("No changes produced by the agent (empty git diff and no untracked files)")
	}
	if len(diffOutput) > 0 {
		log.Printf("Changes:\n%s", string(diffOutput))
	}
	if len(statusOutput) > 0 {
		log.Printf("Status:\n%s", string(statusOutput))
	}

	// 9a. Internal code review (re-runs go build/vet externally and
	// makes one Gemini Flash call for semantic checks). See #33.
	log.Printf("Running internal code reviewer...")
	verdict, err := codereviewer.Review(ctx, geminiKey, repoDir, fullIssue, plan, dossier)
	if err != nil {
		log.Fatalf("code reviewer failed: %v", err)
	}
	log.Printf("Code review verdict: approved=%v category=%q summary=%s",
		verdict.Approved, verdict.Category, verdict.Summary)

	// reviewRetried records whether the retry path was consumed so the
	// final artifact write reports it accurately — even on the path
	// where the retry succeeded and the second review approved.
	reviewRetried := false

	if !verdict.Approved {
		reviewRetried = true
		log.Printf("Code review rejected: %s", verdict.Feedback)
		log.Printf("Retrying implementer with reviewer feedback...")

		retryPlan := &planner.ImplementationPlan{
			Markdown: plan.Markdown +
				"\n\n## Reviewer Feedback\n\nThe previous implementation was rejected:\n\n" +
				verdict.Feedback +
				"\n\nFix the issues above. The build and vet checks will be re-run.",
		}

		// Derive the remaining budget so total spend across the
		// original run + retry stays bounded by IMPL_MAX_COST (the
		// contract the PR description promised). RunAgent enforces
		// maxCost per invocation, so passing implMaxCost unchanged
		// would let a run that spent 90% of the cap spend nearly the
		// whole cap again on retry.
		//
		// implMaxCost == 0 means "no limit" — preserve that by
		// passing 0 through unchanged.
		retryBudget := implMaxCost
		if implMaxCost > 0 {
			costModel := modelName
			if costModel == "" {
				costModel = "claude-haiku-4-5-20251001"
			}
			firstRunCost := cost.CalculateWithCache(costModel,
				result.InputTokens, result.CacheCreationTokens,
				result.CacheReadTokens, result.OutputTokens)
			retryBudget = implMaxCost - firstRunCost
			if retryBudget <= 0 {
				log.Printf("No budget remaining after first run ($%.4f / $%.4f) — halting without retry",
					firstRunCost, implMaxCost)
				result.BudgetExceeded = true
				if artifactDir != "" {
					writeRunArtifacts(artifactDir, issue, result, modelName, plan)
				}
				writeCodeReviewArtifact(artifactDir, verdict, true)
				os.RemoveAll(repoDir)
				os.RemoveAll(dossierDir)
				os.Exit(1)
			}
			log.Printf("Retry budget: $%.4f remaining (spent $%.4f on first pass of $%.4f cap)",
				retryBudget, firstRunCost, implMaxCost)
		}

		retryResult, rerr := implementer.RunAgent(ctx, anthropicKey, modelName,
			repoDir, retryPlan, maxIter, retryBudget)
		if rerr != nil {
			writeCodeReviewArtifact(artifactDir, verdict, true)
			log.Fatalf("implementer retry failed: %v", rerr)
		}
		log.Printf("Retry completed in %d iterations", retryResult.Iterations)

		// Merge retry token counts into the primary result so
		// run-summary.json reflects total implementer spend.
		result.Iterations += retryResult.Iterations
		result.InputTokens += retryResult.InputTokens
		result.OutputTokens += retryResult.OutputTokens
		result.CacheCreationTokens += retryResult.CacheCreationTokens
		result.CacheReadTokens += retryResult.CacheReadTokens
		if retryResult.BudgetExceeded {
			result.BudgetExceeded = true
		}

		// Refresh run-summary.json with the merged totals so top-level
		// iterations / token counts / estimated_cost_usd reflect the
		// retry, not the stale first-run values written at L188.
		if artifactDir != "" {
			writeRunArtifacts(artifactDir, issue, result, modelName, plan)
		}

		if result.BudgetExceeded {
			log.Printf("Implementer budget exceeded during retry (IMPL_MAX_COST=$%.4f) — halting", implMaxCost)
			writeCodeReviewArtifact(artifactDir, verdict, true)
			os.RemoveAll(repoDir)
			os.RemoveAll(dossierDir)
			os.Exit(1)
		}

		// Re-review after the retry. Pass retryPlan (not the original
		// plan) so the semantic reviewer can see the specific feedback
		// the retry was asked to address and verify it was applied —
		// otherwise a retry could be approved even if it ignored every
		// requested fix.
		verdict, err = codereviewer.Review(ctx, geminiKey, repoDir, fullIssue, retryPlan, dossier)
		if err != nil {
			// Write a partial verdict so operators can diagnose why the
			// pipeline halted after the retry was already consumed.
			// verdict is nil when Review errors, so construct one here.
			writeCodeReviewArtifact(artifactDir, &codereviewer.Verdict{
				Approved: false,
				Category: "reviewer_error",
				Summary:  fmt.Sprintf("code reviewer (retry) failed: %v", err),
			}, true)
			log.Fatalf("code reviewer (retry) failed: %v", err)
		}
		log.Printf("Retry code review verdict: approved=%v category=%q summary=%s",
			verdict.Approved, verdict.Category, verdict.Summary)
		if !verdict.Approved {
			log.Printf("Code review still rejected after retry: %s", verdict.Feedback)
			writeCodeReviewArtifact(artifactDir, verdict, true)
			log.Fatalf("halting before PR creation — code review failed twice")
		}
		log.Printf("Code review approved after retry")
	}

	// Record the final verdict. Must happen before step 10 so the
	// artifact reflects review state even if PR upsert fails.
	// reviewRetried preserves the retry signal on the
	// successful-after-retry path.
	writeCodeReviewArtifact(artifactDir, verdict, reviewRetried)

	// 10. Create or update branch and draft PR (handles collisions)
	branch := fmt.Sprintf("agent/fix-%d", issue.Number)
	commitMsg := fmt.Sprintf("fix: %s\n\nFixes #%d\n\nGenerated by conduit-agent-experiment implementer.", issue.Title, issue.Number)

	modelDisplay := modelName
	if modelDisplay == "" {
		modelDisplay = "Haiku 4.5"
	}

	prInput := github.DraftPRInput{
		Title: fmt.Sprintf("fix: %s", issue.Title),
		Body:  fmt.Sprintf("Fixes #%d\n\n## Agent Summary\n\n%s\n\n---\nGenerated by conduit-agent-experiment (archivist: Gemini Flash, implementer: %s, %d iterations).", issue.Number, result.Summary, modelDisplay, result.Iterations),
		Base:  "main",
	}

	upsert, err := adapter.UpsertBranchAndPR(ctx, repoDir, branch, commitMsg, prInput)
	if err != nil {
		log.Fatalf("upserting branch and PR: %v", err)
	}
	prURL := upsert.PRURL
	branch = upsert.Branch // may be suffixed

	switch upsert.Action {
	case github.UpsertSkippedMerged:
		log.Printf("Skipping PR: branch %s already has a merged PR upstream", branch)
	case github.UpsertUpdated:
		log.Printf("Updated existing PR: %s", prURL)
	case github.UpsertSuffixed:
		log.Printf("Created suffixed PR on %s: %s", branch, prURL)
	case github.UpsertForcePushed:
		log.Printf("Force-pushed orphan branch %s, new PR: %s", branch, prURL)
	case github.UpsertCreated:
		log.Printf("Draft PR created: %s", prURL)
	default:
		log.Printf("PR upserted (%s): %s", upsert.Action, prURL)
	}

	// Update artifact with PR URL (skipped when no PR was created)
	if prURL != "" && artifactDir != "" {
		appendPRURL(artifactDir, prURL)
	}

	// 11. Gate 3: Bot review loop + human approval (HITL)
	// Skipped when there's no PR (upsert skipped a merged branch).
	if hitlCfg.Gate3Enabled && prURL != "" {
		log.Printf("[HITL] Gate 3 active — starting bot review loop on PR %s", prURL)

		prNum := extractPRNumber(prURL)
		if prNum == 0 {
			log.Fatalf("[HITL] could not extract PR number from URL: %s", prURL)
		}

		for botIter := 1; botIter <= hitlCfg.BotMaxIterations; botIter++ {
			log.Printf("[HITL] Bot review iteration %d/%d", botIter, hitlCfg.BotMaxIterations)

			if err := hitl.TriggerBotReviews(ctx, hitlAdapter, prNum, hitlCfg.BotReviewers); err != nil {
				log.Printf("[HITL] Warning: failed to trigger bot reviews: %v", err)
			}

			log.Printf("[HITL] Waiting %v for bot reviews...", hitlCfg.BotReviewWait)
			time.Sleep(hitlCfg.BotReviewWait)

			commentData, err := fetchPRComments(ctx, adapter, prNum)
			if err != nil {
				log.Printf("[HITL] Warning: failed to fetch PR comments: %v", err)
				continue
			}

			comments, err := responder.ParseInlineComments(commentData)
			if err != nil {
				log.Printf("[HITL] Warning: failed to parse comments: %v", err)
				continue
			}

			actionable := responder.Classify(comments)
			if len(actionable) == 0 {
				log.Printf("[HITL] No actionable bot comments, bot loop complete")
				break
			}

			log.Printf("[HITL] Found %d actionable comments, running fix agent...", len(actionable))
			prompt := responder.BuildFixPrompt(actionable)
			fixPlan := &planner.ImplementationPlan{Markdown: prompt}

			fixResult, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, fixPlan, maxIter, implMaxCost)
			if err != nil {
				log.Printf("[HITL] Fix agent failed: %v", err)
				continue
			}
			log.Printf("[HITL] Fix agent completed in %d iterations", fixResult.Iterations)
			if fixResult.BudgetExceeded {
				log.Printf("[HITL] Fix agent budget exceeded, ending bot review loop")
				break
			}

			statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
			statusCmd.Dir = repoDir
			statusOutput, err := statusCmd.Output()
			if err != nil || len(statusOutput) == 0 {
				log.Printf("[HITL] No changes from fix agent")
				break
			}

			commitMsg := fmt.Sprintf("fix: address bot review comments (iteration %d)", botIter)
			pushRemote := "origin"
			if adapter.ForkOwner != "" && adapter.ForkOwner != adapter.Owner {
				pushRemote = "fork"
			}
			pushCmds := [][]string{
				{"git", "add", "-A"},
				{"git", "commit", "-m", commitMsg},
				{"git", "push", pushRemote, branch},
			}
			pushFailed := false
			for _, args := range pushCmds {
				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				cmd.Dir = repoDir
				if out, err := cmd.CombinedOutput(); err != nil {
					log.Printf("[HITL] %s failed: %v\n%s", args[0], err, out)
					pushFailed = true
					break
				}
			}
			if pushFailed {
				continue
			}

			if hitlCfg.ResolveBotComments {
				resolved, err := hitl.ResolveAllThreads(ctx, hitlAdapter, prNum)
				if err != nil {
					log.Printf("[HITL] Warning: failed to resolve threads: %v", err)
				} else {
					log.Printf("[HITL] Resolved %d review threads", resolved)
				}
			}
		}

		if err := hitlAdapter.AddLabel(ctx, prNum, hitl.LabelReadyForReview); err != nil {
			log.Printf("[HITL] Warning: failed to apply ready-for-review label: %v", err)
		}
		log.Printf("[HITL] Bot review loop complete. Waiting for human action on PR #%d...", prNum)

		action, err := hitl.WaitForPRAction(ctx, hitlAdapter, prNum, hitlCfg.Gate3PollInterval)
		if err != nil {
			log.Fatalf("[HITL] Gate 3 error: %v", err)
		}

		switch action {
		case "merged", "approved":
			log.Printf("[HITL] PR #%d %s by human", prNum, action)
		case "changes_requested":
			log.Printf("[HITL] PR #%d has changes requested — run `make respond RESPONDER_PR_NUMBER=%d` to address", prNum, prNum)
		case "closed":
			log.Printf("[HITL] PR #%d closed by human", prNum)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readTopRankedIssue(dir string) (*triage.RankedIssue, error) {
	files, err := filepath.Glob(filepath.Join(dir, "triage-*.json"))
	if err != nil {
		return nil, fmt.Errorf("globbing triage files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no triage files found in %s", dir)
	}
	sort.Strings(files)
	latest := files[len(files)-1]

	data, err := os.ReadFile(latest)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", latest, err)
	}

	var output triage.TriageOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", latest, err)
	}
	if len(output.Ranked) == 0 {
		return nil, fmt.Errorf("no ranked issues in %s", latest)
	}

	return &output.Ranked[0], nil
}

func findIssueByNumber(dir string, number int) (*triage.RankedIssue, error) {
	files, err := filepath.Glob(filepath.Join(dir, "triage-*.json"))
	if err != nil {
		return nil, fmt.Errorf("globbing triage files: %w", err)
	}
	sort.Strings(files)
	for i := len(files) - 1; i >= 0; i-- {
		data, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		var output triage.TriageOutput
		if err := json.Unmarshal(data, &output); err != nil {
			continue
		}
		for j := range output.Ranked {
			if output.Ranked[j].Number == number {
				return &output.Ranked[j], nil
			}
		}
	}
	return nil, fmt.Errorf("issue #%d not found in triage files in %s", number, dir)
}

func cloneRepo(ctx context.Context, owner, repo string) (string, error) {
	dir, err := os.MkdirTemp("", "implementer-*")
	if err != nil {
		return "", err
	}
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", repoURL, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w\n%s", err, output)
	}
	return dir, nil
}

func extractPRNumber(prURL string) int {
	prURL = strings.TrimRight(prURL, "/")
	parts := strings.Split(prURL, "/")
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

// writeRunArtifacts writes run metadata and cost info to the artifact directory
// for collection by GitHub Actions.
func writeRunArtifacts(dir string, issue *triage.RankedIssue, result *implementer.Result, modelName string, plan *planner.ImplementationPlan) {
	model := modelName
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	summary := map[string]any{
		"issue_number":          issue.Number,
		"issue_title":           issue.Title,
		"model":                 model,
		"iterations":            result.Iterations,
		"input_tokens":          result.InputTokens,
		"output_tokens":         result.OutputTokens,
		"cache_creation_tokens": result.CacheCreationTokens,
		"cache_read_tokens":     result.CacheReadTokens,
		"budget_exceeded":       result.BudgetExceeded,
		"summary":               result.Summary,
		"plan_chars":            len(plan.Markdown),
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}

	// Estimate cost using the pricing package
	implCost := cost.Calculate(model, result.InputTokens, result.OutputTokens)
	summary["estimated_cost_usd"] = implCost

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		log.Printf("Warning: failed to marshal run summary: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "run-summary.json"), data, 0644); err != nil {
		log.Printf("Warning: failed to write run-summary.json: %v", err)
		return
	}
	log.Printf("Artifacts written to %s", dir)
}

// appendPRURL updates the run summary artifact with the PR URL after creation.
func appendPRURL(dir string, prURL string) {
	path := filepath.Join(dir, "run-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: failed to read run-summary.json for PR URL update: %v", err)
		return
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		log.Printf("Warning: failed to parse run-summary.json: %v", err)
		return
	}
	summary["pr_url"] = prURL
	updated, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, updated, 0644)
}

func fetchPRComments(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"api",
		fmt.Sprintf("repos/%s/%s/pulls/%d/comments", adapter.Owner, adapter.Repo, prNum),
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w\n%s", err, stderr.String())
	}
	return out, nil
}

// writeCodeReviewArtifact merges the code review verdict into
// run-summary.json under a "code_review" key. Mirrors the appendPRURL
// pattern: read JSON, merge, write back. No-op when dir is empty.
//
// retried indicates whether the retry path was consumed during this
// run (true = this verdict is the result of the second attempt).
func writeCodeReviewArtifact(dir string, verdict *codereviewer.Verdict, retried bool) {
	if dir == "" || verdict == nil {
		return
	}
	path := filepath.Join(dir, "run-summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: failed to read run-summary.json for code-review update: %v", err)
		return
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		log.Printf("Warning: failed to parse run-summary.json: %v", err)
		return
	}
	// Derive build_passed / vet_passed from category by explicitly
	// enumerating the success cases rather than subtracting failure
	// categories. This keeps unknown categories (e.g. reviewer_error)
	// conservatively false instead of silently reporting both gates
	// as passed, and makes the semantics obvious at a glance:
	//
	//	""         — build + vet + semantic all passed
	//	"vet"      — build passed, vet failed, semantic not run
	//	"semantic" — build + vet passed, semantic rejected
	//	"build"    — build failed, vet not run, semantic not run
	//	other      — runner/reviewer error; stage state unknown
	buildPassed := verdict.Category == "" || verdict.Category == "vet" || verdict.Category == "semantic"
	vetPassed := verdict.Category == "" || verdict.Category == "semantic"
	summary["code_review"] = map[string]any{
		"approved":        verdict.Approved,
		"category":        verdict.Category,
		"summary":         verdict.Summary,
		"retried":         retried,
		"build_passed":    buildPassed,
		"vet_passed":      vetPassed,
		"semantic_result": verdict.SemanticResult,
		"input_tokens":    verdict.InputTokens,
		"output_tokens":   verdict.OutputTokens,
		"cost_usd":        verdict.CostUSD,
	}
	updated, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		log.Printf("Warning: failed to marshal updated run-summary: %v", err)
		return
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		log.Printf("Warning: failed to write updated run-summary: %v", err)
	}
}
