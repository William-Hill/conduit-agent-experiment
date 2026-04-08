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

	for iteration := 1; iteration <= maxIterations; iteration++ {
		log.Printf("=== Responder iteration %d/%d ===", iteration, maxIterations)

		// 1. Fetch review comments
		commentData, err := fetchPRComments(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("fetching comments: %v", err)
		}

		// 2. Check for approval
		reviewData, err := fetchPRReviews(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("fetching reviews: %v", err)
		}
		approved, err := responder.HasApproval(reviewData)
		if err != nil {
			log.Fatalf("parsing reviews: %v", err)
		}
		if approved {
			log.Printf("PR #%d has been approved, exiting", prNum)
			return
		}

		// 3. Parse and classify comments
		comments, err := responder.ParseInlineComments(commentData)
		if err != nil {
			log.Fatalf("parsing comments: %v", err)
		}
		actionable := responder.Classify(comments)
		if len(actionable) == 0 {
			log.Printf("No actionable comments remaining, exiting")
			return
		}
		log.Printf("Found %d actionable comments (of %d total)", len(actionable), len(comments))

		// 4. Get the PR branch name
		branch, err := getPRBranch(ctx, adapter, prNum)
		if err != nil {
			log.Fatalf("getting PR branch: %v", err)
		}

		// 5. Clone and checkout the PR branch
		repoDir, err := cloneAndCheckout(ctx, owner, repo, forkOwner, branch)
		if err != nil {
			log.Fatalf("cloning repo: %v", err)
		}

		// 6. Run fix agent
		prompt := responder.BuildFixPrompt(actionable)
		plan := &planner.ImplementationPlan{Markdown: prompt}
		log.Printf("Running fix agent on %d comments...", len(actionable))

		result, err := implementer.RunAgent(ctx, anthropicKey, modelName, repoDir, plan, maxToolIter, 0)
		if err != nil {
			log.Fatalf("fix agent failed: %v", err)
		}
		log.Printf("Fix agent completed in %d iterations", result.Iterations)
		log.Printf("Summary: %s", result.Summary)

		// 7. Check for changes
		diffCmd := exec.CommandContext(ctx, "git", "diff", "--stat")
		diffCmd.Dir = repoDir
		diffOutput, err := diffCmd.Output()
		if err != nil {
			log.Fatalf("git diff failed: %v", err)
		}
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = repoDir
		statusOutput, err := statusCmd.Output()
		if err != nil {
			log.Fatalf("git status failed: %v", err)
		}
		if len(diffOutput) == 0 && len(statusOutput) == 0 {
			log.Printf("No changes produced, skipping push")
			os.RemoveAll(repoDir)
			continue
		}
		log.Printf("Changes:\n%s", string(diffOutput))

		// 8. Commit and push
		commitMsg := fmt.Sprintf("fix: address review comments (responder iteration %d)", iteration)
		if err := commitAndPush(ctx, repoDir, branch, forkOwner, owner, commitMsg); err != nil {
			log.Fatalf("commit and push failed: %v", err)
		}
		log.Printf("Pushed iteration %d", iteration)
		os.RemoveAll(repoDir)

		// 9. Wait for new reviews
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
	return cmd.Output()
}

func fetchPRReviews(ctx context.Context, adapter *github.Adapter, prNum int) ([]byte, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "reviews",
		"--jq", ".reviews",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	return cmd.Output()
}

func getPRBranch(ctx context.Context, adapter *github.Adapter, prNum int) (string, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNum),
		"--repo", fmt.Sprintf("%s/%s", adapter.Owner, adapter.Repo),
		"--json", "headRefName",
		"--jq", ".headRefName",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func cloneAndCheckout(ctx context.Context, owner, repo, forkOwner, branch string) (string, error) {
	dir, err := os.MkdirTemp("", "responder-*")
	if err != nil {
		return "", err
	}

	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", forkOwner, repo)
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--depth", "50", repoURL, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w\n%s", err, out)
	}

	return dir, nil
}

func commitAndPush(ctx context.Context, repoDir, branch, forkOwner, owner, commitMsg string) error {
	cmds := [][]string{
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
