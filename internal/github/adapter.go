package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// CreateBranchAndPush creates a branch, stages all changes, commits and pushes
// in the given worktree directory.
func (a *Adapter) CreateBranchAndPush(ctx context.Context, worktreeDir, branch, commitMsg string) error {
	cmds := [][]string{
		{"git", "checkout", "-B", branch},
		{"git", "add", "-A"},
		{"git", "commit", "-m", commitMsg},
	}

	// Determine push remote: if fork differs from upstream, add fork as a remote
	pushRemote := "origin"
	if a.ForkOwner != "" && a.ForkOwner != a.Owner {
		forkURL := fmt.Sprintf("https://github.com/%s/%s.git", a.ForkOwner, a.Repo)
		// Add fork remote (ignore error if already exists)
		addRemote := exec.CommandContext(ctx, "git", "remote", "add", "fork", forkURL)
		addRemote.Dir = worktreeDir
		addRemote.CombinedOutput() // ignore error — remote may already exist
		pushRemote = "fork"
	}

	cmds = append(cmds, []string{"git", "push", "-u", pushRemote, branch})

	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = worktreeDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("running %s: %w\n%s", strings.Join(args, " "), err, out)
		}
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

