package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Label represents a GitHub issue label.
type Label struct {
	Name string `json:"name"`
}

// Issue represents a GitHub issue.
type Issue struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	Labels    []Label `json:"labels"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"createdAt"`
	Comments  []any   `json:"comments"`
	Assignees []any   `json:"assignees"`
}

// IssueListOpts contains options for listing issues.
type IssueListOpts struct {
	Limit  int
	Labels []string
}

// DraftPRInput contains inputs for creating a draft PR.
type DraftPRInput struct {
	Title string
	Body  string
	Head  string // branch name on fork
	Base  string // target branch on upstream
}

// UpsertAction describes what UpsertBranchAndPR did. The five variants
// correspond to distinct starting states:
//   - UpsertCreated: branch didn't exist, fresh push + new PR
//   - UpsertForcePushed: branch existed but had no PR (orphan), force-pushed + new PR
//   - UpsertUpdated: branch had an open PR, force-pushed + comment on existing PR
//   - UpsertSuffixed: branch had a closed PR, pushed under -N suffix + new PR
//   - UpsertSkippedMerged: branch had a merged PR, no push, no PR
type UpsertAction string

const (
	UpsertCreated       UpsertAction = "created"        // fresh branch + new PR
	UpsertForcePushed   UpsertAction = "force_pushed"   // branch existed but no PR, force-pushed + new PR
	UpsertUpdated       UpsertAction = "updated"        // force-pushed + commented on existing open PR
	UpsertSuffixed      UpsertAction = "suffixed"       // prior PR closed, new branch with -N suffix + new PR
	UpsertSkippedMerged UpsertAction = "skipped_merged" // prior PR merged, no push, no PR
)

// UpsertResult is returned by UpsertBranchAndPR.
type UpsertResult struct {
	PRURL  string       // empty iff Action == UpsertSkippedMerged
	Branch string       // final branch name (may differ from input if suffixed)
	Action UpsertAction // which decision branch was taken
}

// Adapter wraps the gh CLI for GitHub operations.
type Adapter struct {
	Owner      string
	Repo       string
	BaseBranch string
	ForkOwner  string
	GHPath     string // path to gh binary, defaults to "gh"
}

func (a *Adapter) ghPath() string {
	if a.GHPath != "" {
		return a.GHPath
	}
	return "gh"
}

func (a *Adapter) repo() string {
	return a.Owner + "/" + a.Repo
}

func (a *Adapter) forkRepo() string {
	return a.ForkOwner + "/" + a.Repo
}

const issueFields = "number,title,labels,body,createdAt,comments,assignees"

// ListIssues lists open GitHub issues using the gh CLI.
func (a *Adapter) ListIssues(ctx context.Context, opts IssueListOpts) ([]Issue, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 30
	}

	args := []string{
		"issue", "list",
		"--repo", a.repo(),
		"--state", "open",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", issueFields,
	}

	for _, label := range opts.Labels {
		args = append(args, "--label", label)
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}

	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("parsing issue list output: %w", err)
	}

	return issues, nil
}

// GetIssue fetches a single GitHub issue by number.
func (a *Adapter) GetIssue(ctx context.Context, number int) (*Issue, error) {
	args := []string{
		"issue", "view",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--json", issueFields,
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue view %d: %w", number, err)
	}

	var issue Issue
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		return nil, fmt.Errorf("parsing issue output: %w", err)
	}

	return &issue, nil
}

// branchExistsOnFork returns true if the branch exists on the fork. A 404
// from the GitHub API is interpreted as "not exists" (nil error). Any other
// error is returned as-is.
func (a *Adapter) branchExistsOnFork(ctx context.Context, branch string) (bool, error) {
	_, err := a.runGH(ctx, "api", fmt.Sprintf("repos/%s/branches/%s", a.forkRepo(), branch), "--silent")
	if err == nil {
		return true, nil
	}
	// gh surfaces "HTTP 404" in stderr for not-found. runGH wraps stderr into the error string.
	if strings.Contains(err.Error(), "HTTP 404") {
		return false, nil
	}
	return false, fmt.Errorf("gh api repos/%s/branches/%s: %w", a.forkRepo(), branch, err)
}

// prSummary is a small PR summary used by the upsert logic.
type prSummary struct {
	Number    int    `json:"number"`
	State     string `json:"state"` // OPEN, CLOSED, MERGED
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

// mostRecentPRForBranch returns the most recent PR (by createdAt) whose head
// matches <fork-owner>:<branch>. Returns (nil, nil) when no PR exists.
func (a *Adapter) mostRecentPRForBranch(ctx context.Context, branch string) (*prSummary, error) {
	head := branch
	if a.ForkOwner != "" && a.ForkOwner != a.Owner {
		head = a.ForkOwner + ":" + branch
	}
	args := []string{
		"pr", "list",
		"--repo", a.repo(),
		"--head", head,
		"--state", "all",
		"--json", "number,state,url,createdAt",
		"--limit", "20",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr list --head %s: %w", head, err)
	}
	var prs []prSummary
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("parsing pr list output: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	// Sort descending by CreatedAt (RFC3339 lexicographic sort works).
	sort.Slice(prs, func(i, j int) bool { return prs[i].CreatedAt > prs[j].CreatedAt })
	return &prs[0], nil
}

// ensureForkRemote adds the "fork" git remote in worktreeDir if the fork
// differs from upstream. Idempotent — existing remotes are left alone.
// Returns the name of the push remote ("fork" if fork differs, "origin" otherwise).
func (a *Adapter) ensureForkRemote(ctx context.Context, worktreeDir string) string {
	if a.ForkOwner == "" || a.ForkOwner == a.Owner {
		return "origin"
	}
	forkURL := fmt.Sprintf("https://github.com/%s/%s.git", a.ForkOwner, a.Repo)
	addRemote := exec.CommandContext(ctx, "git", "remote", "add", "fork", forkURL)
	addRemote.Dir = worktreeDir
	addRemote.CombinedOutput() // ignore error — remote may already exist
	return "fork"
}

// commitWorktree runs `git checkout -B <branch>`, `git add -A`, `git commit -m`
// in worktreeDir. Returns an error if any step fails.
func (a *Adapter) commitWorktree(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	cmds := [][]string{
		{"git", "checkout", "-B", branch},
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = worktreeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("running %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// forcePushBranch fetches the current state of <branch> from the fork, then
// force-pushes the local HEAD to it using --force-with-lease against the
// just-fetched sha. This means we overwrite the remote branch only if it
// still matches what we just saw, protecting against races.
func (a *Adapter) forcePushBranch(ctx context.Context, worktreeDir, branch string) error {
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)

	// Fetch the branch so we have a remote-tracking ref for --force-with-lease.
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", pushRemote, branch)
	fetchCmd.Dir = worktreeDir
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch %s %s: %w\n%s", pushRemote, branch, err, out)
	}

	// Read the fetched remote sha to pin --force-with-lease.
	revCmd := exec.CommandContext(ctx, "git", "rev-parse", pushRemote+"/"+branch)
	revCmd.Dir = worktreeDir
	shaBytes, err := revCmd.Output()
	if err != nil {
		return fmt.Errorf("git rev-parse %s/%s: %w", pushRemote, branch, err)
	}
	expectedSha := strings.TrimSpace(string(shaBytes))

	lease := fmt.Sprintf("--force-with-lease=%s:%s", branch, expectedSha)
	pushCmd := exec.CommandContext(ctx, "git", "push", lease, pushRemote, "HEAD:"+branch)
	pushCmd.Dir = worktreeDir
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push --force-with-lease %s %s: %w\n%s", pushRemote, branch, err, out)
	}
	return nil
}

// UpsertBranchAndPR creates or updates a branch on the fork and its draft PR,
// handling the cases where the branch or a prior PR already exists. See
// docs/superpowers/specs/2026-04-09-pr-upsert-branch-collision-design.md for
// the full decision tree.
//
// prInput.Head is ignored — the method sets it to the final branch name.
func (a *Adapter) UpsertBranchAndPR(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	return a.upsertWithDepth(ctx, worktreeDir, branch, commitMsg, prInput, 0)
}

// maxUpsertSuffixDepth bounds the suffix search when prior PRs are closed.
// -2, -3, ..., -10 → depth 9.
const maxUpsertSuffixDepth = 9

func (a *Adapter) upsertWithDepth(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
	depth int,
) (UpsertResult, error) {
	exists, err := a.branchExistsOnFork(ctx, branch)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("checking branch exists: %w", err)
	}
	if !exists {
		return a.createFresh(ctx, worktreeDir, branch, commitMsg, prInput)
	}

	pr, err := a.mostRecentPRForBranch(ctx, branch)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("looking up most recent PR: %w", err)
	}

	switch {
	case pr != nil && pr.State == "OPEN":
		return a.updateExisting(ctx, worktreeDir, branch, commitMsg, pr)
	case pr != nil && pr.State == "MERGED":
		log.Printf("UpsertBranchAndPR: branch %s has merged PR #%d, skipping push", branch, pr.Number)
		return UpsertResult{Branch: branch, Action: UpsertSkippedMerged}, nil
	case pr != nil && pr.State == "CLOSED":
		return a.recurseSuffixed(ctx, worktreeDir, branch, commitMsg, prInput, depth)
	case pr == nil:
		// Branch exists but no PR ever — treat as orphan, force-push and create.
		return a.forcePushNew(ctx, worktreeDir, branch, commitMsg, prInput)
	default:
		return UpsertResult{}, fmt.Errorf("branch %s PR state %q not recognized", branch, pr.State)
	}
}

// updateExisting commits, force-pushes, and posts a timestamped "Updated by
// automated run" comment on the existing open PR.
func (a *Adapter) updateExisting(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	pr *prSummary,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	if err := a.forcePushBranch(ctx, worktreeDir, branch); err != nil {
		return UpsertResult{}, fmt.Errorf("force push: %w", err)
	}
	body := fmt.Sprintf("Updated by automated run at %s", time.Now().UTC().Format(time.RFC3339))
	if err := a.PostComment(ctx, pr.Number, body); err != nil {
		return UpsertResult{}, fmt.Errorf("post update comment: %w", err)
	}
	return UpsertResult{PRURL: pr.URL, Branch: branch, Action: UpsertUpdated}, nil
}

// recurseSuffixed handles the CLOSED case by recursing into the next suffix.
// It strips any existing -N suffix from the current branch name and increments.
func (a *Adapter) recurseSuffixed(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
	depth int,
) (UpsertResult, error) {
	if depth >= maxUpsertSuffixDepth {
		return UpsertResult{}, fmt.Errorf("upsert suffix cap exceeded: tried %d variations of branch %s, all had closed PRs", maxUpsertSuffixDepth+1, branch)
	}
	base, cur := parseSuffix(branch)
	next := cur + 1
	if next < 2 {
		next = 2
	}
	nextBranch := fmt.Sprintf("%s-%d", base, next)
	result, err := a.upsertWithDepth(ctx, worktreeDir, nextBranch, commitMsg, prInput, depth+1)
	if err != nil {
		return result, err
	}
	// Any terminal outcome at the suffixed name is reported as Suffixed so
	// callers can tell the suffix logic kicked in, EXCEPT skipped-merged which
	// we propagate as-is (the work is already done upstream).
	if result.Action != UpsertSkippedMerged {
		result.Action = UpsertSuffixed
	}
	return result, nil
}

// forcePushNew handles "branch exists but no PR ever" — force-push and create.
func (a *Adapter) forcePushNew(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	if err := a.forcePushBranch(ctx, worktreeDir, branch); err != nil {
		return UpsertResult{}, fmt.Errorf("force push: %w", err)
	}
	prInput.Head = branch
	url, err := a.createDraftPR(ctx, prInput)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("create draft PR: %w", err)
	}
	return UpsertResult{PRURL: url, Branch: branch, Action: UpsertForcePushed}, nil
}

// parseSuffix splits a branch name like "agent/fix-1" into ("agent/fix-1", 0)
// or "agent/fix-1-3" into ("agent/fix-1", 3). The "base" never ends in -N for
// N >= 2; the first call on a fresh branch returns depth 0 and produces -2.
var suffixRe = regexp.MustCompile(`^(.*)-(\d+)$`)

func parseSuffix(branch string) (base string, n int) {
	m := suffixRe.FindStringSubmatch(branch)
	if m == nil {
		return branch, 0
	}
	// Only treat trailing -N as a suffix when 2 <= N <= maxUpsertSuffixDepth+1.
	// This distinguishes recursion suffixes (-2 through -10) from issue numbers
	// embedded in branch names (e.g. agent/fix-1268 has issue number 1268).
	var parsed int
	if _, err := fmt.Sscanf(m[2], "%d", &parsed); err != nil || parsed < 2 || parsed > maxUpsertSuffixDepth+1 {
		return branch, 0
	}
	return m[1], parsed
}

// createFresh commits, pushes, and creates a draft PR for a branch that does
// not already exist on the fork.
func (a *Adapter) createFresh(
	ctx context.Context,
	worktreeDir string,
	branch string,
	commitMsg string,
	prInput DraftPRInput,
) (UpsertResult, error) {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return UpsertResult{}, fmt.Errorf("commit worktree: %w", err)
	}
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)
	cmd := exec.CommandContext(ctx, "git", "push", "-u", pushRemote, branch)
	cmd.Dir = worktreeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return UpsertResult{}, fmt.Errorf("git push -u %s %s: %w\n%s", pushRemote, branch, err, out)
	}
	prInput.Head = branch
	url, err := a.createDraftPR(ctx, prInput)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("create draft PR: %w", err)
	}
	return UpsertResult{PRURL: url, Branch: branch, Action: UpsertCreated}, nil
}

// createDraftPR creates a draft PR via the gh CLI and returns the PR URL.
func (a *Adapter) createDraftPR(ctx context.Context, input DraftPRInput) (string, error) {
	head := input.Head
	if a.ForkOwner != "" {
		head = a.ForkOwner + ":" + input.Head
	}

	args := []string{
		"pr", "create",
		"--repo", a.repo(),
		"--title", input.Title,
		"--body", input.Body,
		"--head", head,
		"--base", input.Base,
		"--draft",
	}

	out, err := a.runGH(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w", err)
	}

	return strings.TrimSpace(out), nil
}

// PRState represents the current state of a pull request.
type PRState struct {
	State          string `json:"state"`          // OPEN, CLOSED, MERGED
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
}

// AddLabel adds a label to an issue or PR.
func (a *Adapter) AddLabel(ctx context.Context, number int, label string) error {
	args := []string{
		"issue", "edit",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--add-label", label,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue edit --add-label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from an issue or PR.
func (a *Adapter) RemoveLabel(ctx context.Context, number int, label string) error {
	args := []string{
		"issue", "edit",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--remove-label", label,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue edit --remove-label: %w", err)
	}
	return nil
}

// GetLabels returns the label names on an issue or PR.
func (a *Adapter) GetLabels(ctx context.Context, number int) ([]string, error) {
	args := []string{
		"issue", "view",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--json", "labels",
		"--jq", ".labels",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh issue view labels: %w", err)
	}

	var labels []Label
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, fmt.Errorf("parsing labels: %w", err)
	}

	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names, nil
}

// PostComment posts a comment on an issue or PR.
func (a *Adapter) PostComment(ctx context.Context, number int, body string) error {
	args := []string{
		"issue", "comment",
		fmt.Sprintf("%d", number),
		"--repo", a.repo(),
		"--body", body,
	}
	_, err := a.runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("gh issue comment: %w", err)
	}
	return nil
}

// GetPRState returns the current state of a pull request.
func (a *Adapter) GetPRState(ctx context.Context, prNumber int) (*PRState, error) {
	args := []string{
		"pr", "view",
		fmt.Sprintf("%d", prNumber),
		"--repo", a.repo(),
		"--json", "state,isDraft,reviewDecision",
	}
	out, err := a.runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var state PRState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return nil, fmt.Errorf("parsing PR state: %w", err)
	}
	return &state, nil
}

// ReviewThread represents a review thread on a PR.
type ReviewThread struct {
	ID         string `json:"id"`
	IsResolved bool   `json:"isResolved"`
	Body       string // first comment body
}

// reviewThreadsResponse is the GraphQL response for review threads.
type reviewThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						Comments   struct {
							Nodes []struct {
								Body string `json:"body"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// GetReviewThreads returns all review threads on a PR using the GraphQL API.
func (a *Adapter) GetReviewThreads(ctx context.Context, prNumber int) ([]ReviewThread, error) {
	query := fmt.Sprintf(`query {
		repository(owner: %q, name: %q) {
			pullRequest(number: %d) {
				reviewThreads(first: 100) {
					nodes {
						id
						isResolved
						comments(first: 1) {
							nodes { body }
						}
					}
				}
			}
		}
	}`, a.Owner, a.Repo, prNumber)

	out, err := a.runGH(ctx, "api", "graphql", "-f", "query="+query)
	if err != nil {
		return nil, fmt.Errorf("gh api graphql (review threads): %w", err)
	}

	var resp reviewThreadsResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parsing review threads: %w", err)
	}

	nodes := resp.Data.Repository.PullRequest.ReviewThreads.Nodes
	threads := make([]ReviewThread, len(nodes))
	for i, n := range nodes {
		body := ""
		if len(n.Comments.Nodes) > 0 {
			body = n.Comments.Nodes[0].Body
		}
		threads[i] = ReviewThread{
			ID:         n.ID,
			IsResolved: n.IsResolved,
			Body:       body,
		}
	}
	return threads, nil
}

// ResolveThread resolves a review thread by its node ID using the GraphQL API.
func (a *Adapter) ResolveThread(ctx context.Context, threadID string) error {
	mutation := fmt.Sprintf(`mutation {
		resolveReviewThread(input: {threadId: %q}) {
			thread { id }
		}
	}`, threadID)

	_, err := a.runGH(ctx, "api", "graphql", "-f", "query="+mutation)
	if err != nil {
		return fmt.Errorf("gh api graphql (resolve thread): %w", err)
	}
	return nil
}

// runGH executes a gh command and returns stdout output.
func (a *Adapter) runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, a.ghPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

