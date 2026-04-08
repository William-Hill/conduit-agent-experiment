package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/hitl"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/implementer"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/responder"
)

func main() {
	ctx := context.Background()

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	prNumber := os.Getenv("RESPONDER_PR_NUMBER")
	if prNumber == "" {
		log.Fatal("RESPONDER_PR_NUMBER is required")
	}
	prNum, err := strconv.Atoi(prNumber)
	if err != nil {
		log.Fatalf("invalid RESPONDER_PR_NUMBER %q: %v", prNumber, err)
	}

	owner := envOrDefault("IMPL_REPO_OWNER", "ConduitIO")
	repo := envOrDefault("IMPL_REPO_NAME", "conduit")
	forkOwner := envOrDefault("IMPL_FORK_OWNER", "William-Hill")
	modelName := os.Getenv("RESPONDER_MODEL")
	maxIterations := envIntOrDefault("RESPONDER_MAX_ITERATIONS", 3)
	waitSeconds := envIntOrDefault("RESPONDER_WAIT_SECONDS", 120)
	maxToolIter := 15

	adapter := &github.Adapter{
		Owner:      owner,
		Repo:       repo,
		ForkOwner:  forkOwner,
		BaseBranch: "main",
	}
	hitlCfg := hitl.LoadConfig()
	hitlAdapter := &github.HITLAdapter{Adapter: adapter}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		log.Printf("=== Responder iteration %d/%d ===", iteration, maxIterations)

		commentData, err := fetchPRComments(ctx, adapter, prNum)
		if err != nil {
			log.Printf("iteration %d: fetching comments failed: %v, skipping", iteration, err)
			continue
		}

		reviewData, err := fetchPRReviews(ctx, adapter, prNum)
		if err != nil {
			log.Printf("iteration %d: fetching reviews failed: %v, skipping", iteration, err)
			continue
		}
		approved, err := responder.HasApproval(reviewData)
		if err != nil {
			log.Printf("iteration %d: parsing reviews failed: %v, skipping", iteration, err)
			continue
		}
		if approved {
			log.Printf("PR #%d has been approved, exiting", prNum)
			return
		}

		comments, err := responder.ParseInlineComments(commentData)
		if err != nil {
			log.Printf("iteration %d: parsing comments failed: %v, skipping", iteration, err)
			continue
		}
		actionable := responder.Classify(comments)
		if len(actionable) == 0 {
			log.Printf("No actionable comments remaining, exiting")
			return
		}
		log.Printf("Found %d actionable comments (of %d total)", len(actionable), len(comments))

		branch, err := getPRBranch(ctx, adapter, prNum)
		if err != nil {
			log.Printf("iteration %d: getting PR branch failed: %v, skipping", iteration, err)
			continue
		}

		repoDir, err := cloneAndCheckout(ctx, forkOwner, repo, branch)
		if err != nil {
			log.Printf("iteration %d: cloning repo failed: %v, skipping", iteration, err)
			continue
		}

		prompt := responder.BuildFixPrompt(actionable)
		plan := &planner.ImplementationPlan{Markdown: prompt}
		log.Printf("Running fix agent on %d comments...", len(actionable))

		result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxToolIter, 0)
		if err != nil {
			log.Printf("iteration %d: fix agent failed: %v, skipping", iteration, err)
			os.RemoveAll(repoDir)
			continue
		}
		log.Printf("Fix agent completed in %d iterations", result.Iterations)
		log.Printf("Summary: %s", result.Summary)

		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = repoDir
		statusOutput, err := statusCmd.Output()
		if err != nil {
			log.Printf("iteration %d: git status failed: %v, skipping", iteration, err)
			os.RemoveAll(repoDir)
			continue
		}
		if len(statusOutput) == 0 {
			log.Printf("No changes produced, skipping push")
			os.RemoveAll(repoDir)
			continue
		}

		diffCmd := exec.CommandContext(ctx, "git", "diff", "--stat")
		diffCmd.Dir = repoDir
		if diffOutput, err := diffCmd.Output(); err == nil && len(diffOutput) > 0 {
			log.Printf("Changes:\n%s", string(diffOutput))
		}

		commitMsg := fmt.Sprintf("fix: address review comments (responder iteration %d)", iteration)
		if err := commitAndPush(ctx, repoDir, branch, commitMsg); err != nil {
			log.Printf("iteration %d: commit and push failed: %v, skipping", iteration, err)
			os.RemoveAll(repoDir)
			continue
		}
		log.Printf("Pushed iteration %d", iteration)

		if hitlCfg.ResolveBotComments {
			resolved, resolveErr := hitl.ResolveAllThreads(ctx, hitlAdapter, prNum)
			if resolveErr != nil {
				log.Printf("Warning: failed to resolve threads: %v", resolveErr)
			} else if resolved > 0 {
				log.Printf("Resolved %d review threads", resolved)
			}
		}

		if len(hitlCfg.BotReviewers) > 0 {
			if triggerErr := hitl.TriggerBotReviews(ctx, hitlAdapter, prNum, hitlCfg.BotReviewers); triggerErr != nil {
				log.Printf("Warning: failed to re-trigger bot reviews: %v", triggerErr)
			} else {
				log.Printf("Re-triggered bot reviews")
			}
		}

		os.RemoveAll(repoDir)

		if iteration < maxIterations {
			log.Printf("Waiting %ds for new reviews...", waitSeconds)
			time.Sleep(time.Duration(waitSeconds) * time.Second)
		}
	}

	log.Printf("Max iterations (%d) reached", maxIterations)
	os.Exit(1)
}

func fetchPRComments(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"api",
		fmt.Sprintf("repos/%s/%s/pulls/%d/comments", adapter.Owner, adapter.Repo, prNum),
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w\n%s", err, out)
	}
	return out, nil
}

func fetchPRReviews(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "reviews",
		"--jq", ".reviews",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w\n%s", err, out)
	}
	return out, nil
}

func getPRBranch(ctx context.Context, adapter *github.Adapter, prNum int) (string, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "headRefName",
		"--jq", ".headRefName",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// cloneAndCheckout uses gh repo clone for automatic credential injection.
func cloneAndCheckout(ctx context.Context, forkOwner, repo, branch string) (string, error) {
	dir, err := os.MkdirTemp("", "responder-*")
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "gh", "repo", "clone",
		fmt.Sprintf("%s/%s", forkOwner, repo), dir,
		"--", "--branch", branch, "--depth", "50")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("gh repo clone: %w\n%s", err, out)
	}

	return dir, nil
}

func commitAndPush(ctx context.Context, repoDir, branch, commitMsg string) error {
	cmds := [][]string{
		{"git", "config", "user.name", "conduit-agent-responder"},
		{"git", "config", "user.email", "conduit-agent-responder@noreply.github.com"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
		{"git", "push", "origin", branch},
	}

	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
