package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
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

// CreateBranchAndPush creates a branch, stages all changes, commits and pushes
// in the given worktree directory.
func (a *Adapter) CreateBranchAndPush(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	if err := a.commitWorktree(ctx, worktreeDir, branch, commitMsg); err != nil {
		return err
	}
	pushRemote := a.ensureForkRemote(ctx, worktreeDir)
	cmd := exec.CommandContext(ctx, "git", "push", "-u", pushRemote, branch)
	cmd.Dir = worktreeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("running git push -u %s %s: %w\n%s", pushRemote, branch, err, out)
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

// CreateDraftPR creates a draft PR via the gh CLI and returns the PR URL.
func (a *Adapter) CreateDraftPR(ctx context.Context, input DraftPRInput) (string, error) {
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

