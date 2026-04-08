package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/implementer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
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

	// 2. Fetch full issue details
	adapter := &github.Adapter{
		Owner:      owner,
		Repo:       repo,
		ForkOwner:  forkOwner,
		BaseBranch: "main",
	}
	fullIssue, err := adapter.GetIssue(ctx, issue.Number)
	if err != nil {
		log.Fatalf("fetching issue #%d: %v", issue.Number, err)
	}

	// 3. Clone repo to temp dir
	repoDir, err := cloneRepo(ctx, owner, repo)
	if err != nil {
		log.Fatalf("cloning repo: %v", err)
	}
	defer os.RemoveAll(repoDir)
	log.Printf("Cloned %s/%s to %s", owner, repo, repoDir)

	// 4. Run archivist (Gemini Flash — cheap exploration)
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

	// 5. Plan implementation (Gemini Flash — writes exact code changes)
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

	// 6. Review plan (Gemini Flash — validates the plan)
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

	// 7. Run implementer agent
	log.Printf("Running implementer agent (max %d iterations)...", maxIter)
	implMaxCost := cost.EnvFloat("IMPL_MAX_COST")
	result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxIter, implMaxCost)
	if err != nil {
		log.Fatalf("agent failed: %v", err)
	}
	log.Printf("Agent completed in %d iterations", result.Iterations)
	log.Printf("Summary: %s", result.Summary)
	if result.BudgetExceeded {
		log.Fatalf("Implementer budget exceeded (IMPL_MAX_COST=$%.4f) — halting before PR creation", implMaxCost)
	}

	// 8. Check for changes (staged, unstaged, and untracked)
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

	// 9. Create branch, commit, push, draft PR
	branch := fmt.Sprintf("agent/fix-%d", issue.Number)
	commitMsg := fmt.Sprintf("fix: %s\n\nFixes #%d\n\nGenerated by conduit-agent-experiment implementer.", issue.Title, issue.Number)
	if err := adapter.CreateBranchAndPush(ctx, repoDir, branch, commitMsg); err != nil {
		log.Fatalf("creating branch and pushing: %v", err)
	}

	modelDisplay := modelName
	if modelDisplay == "" {
		modelDisplay = "Haiku 4.5"
	}

	prURL, err := adapter.CreateDraftPR(ctx, github.DraftPRInput{
		Title: fmt.Sprintf("fix: %s", issue.Title),
		Body:  fmt.Sprintf("Fixes #%d\n\n## Agent Summary\n\n%s\n\n---\nGenerated by conduit-agent-experiment (archivist: Gemini Flash, implementer: %s, %d iterations).", issue.Number, result.Summary, modelDisplay, result.Iterations),
		Head:  branch,
		Base:  "main",
	})
	if err != nil {
		log.Fatalf("creating draft PR: %v", err)
	}

	log.Printf("Draft PR created: %s", prURL)
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
