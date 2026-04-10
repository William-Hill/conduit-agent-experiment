package codereviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/archivist"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/cost"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/github"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/llmutil"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/planner"
	"google.golang.org/genai"
)

const codeReviewSystemPrompt = `You are a code review engineer. You receive a GitHub issue, a plan, a research dossier, and a git diff of an attempted implementation. The code already compiles (go build) and passes go vet.

Check ONLY these semantic concerns:
1. Does the diff actually address the issue in the plan?
2. Are there obvious stubs, TODO/FIXME markers, or unfinished code ("... rest of implementation" etc.)?
3. Are there changes to files unrelated to the plan?
4. Does the diff drop or ignore requirements the plan explicitly called out?

Do NOT flag style, naming, test coverage, or "could be cleaner" concerns — those are for CI and human review. Be strict about stubs and missing work; lenient about everything else.

Output ONLY valid JSON:
{"approved": true, "feedback": "Addresses the issue; no stubs"}
or
{"approved": false, "feedback": "File X is referenced in the plan but the diff doesn't touch it. Also main.go:42 has a TODO stub."}`

// buildReviewPrompt assembles the user-side prompt passed to Gemini
// alongside codeReviewSystemPrompt. Order: issue, plan, dossier,
// touched files, diff (diff last because it's the largest section).
func buildReviewPrompt(issue *github.Issue, plan *planner.ImplementationPlan, dossier *archivist.Dossier, diff string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Issue #%d: %s\n\n%s\n\n", issue.Number, issue.Title, issue.Body)
	fmt.Fprintf(&b, "## Plan\n\n%s\n\n", plan.Markdown)
	fmt.Fprintf(&b, "## Research Summary\n\n%s\n\n", dossier.Summary)
	if dossier.Approach != "" {
		fmt.Fprintf(&b, "## Intended Approach\n\n%s\n\n", dossier.Approach)
	}
	b.WriteString("## Touched Files\n\n")
	if len(files) == 0 {
		b.WriteString("(none detected)\n")
	} else {
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	fmt.Fprintf(&b, "\n## Diff\n\n"+"```"+"diff\n%s\n"+"```"+"\n", diff)
	return b.String()
}

// geminiModel is the model used for the semantic review call. Pricing
// is registered in internal/cost/pricing.go:20.
const geminiModel = "gemini-2.5-flash"

// maxDiffBytes caps the diff fed to the LLM so a giant refactor can't
// blow up the prompt. 32 KiB is ~8k tokens — well under Flash's limit.
const maxDiffBytes = 32 * 1024

// Stage-local timeouts for the non-go-toolchain steps of Review.
// runGo has its own checkTimeout; these cover the git commands in
// collectDiff and the Gemini call in callGeminiForReview so that a
// stuck git index, hung subprocess, or slow model response cannot
// block PR creation indefinitely.
const (
	gitTimeout    = 30 * time.Second
	geminiTimeout = 60 * time.Second
)

// llmVerdict is the JSON shape the semantic LLM returns.
type llmVerdict struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}

// reviewSemantics is a package var so tests can replace it with a stub.
// Default is the real Gemini Flash call in callGeminiForReview.
var reviewSemantics = callGeminiForReview

// callGeminiForReview makes a single gemini-2.5-flash call with the
// system prompt and user prompt, parses the JSON response, and returns
// the verdict plus token usage. Bounded by geminiTimeout so a hung
// model response cannot block PR creation indefinitely.
func callGeminiForReview(ctx context.Context, apiKey, prompt string) (*llmVerdict, int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("creating genai client: %w", err)
	}

	resp, err := client.Models.GenerateContent(ctx, geminiModel, genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{
			genai.NewPartFromText(codeReviewSystemPrompt),
		}},
		ResponseMIMEType: "application/json",
	})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("generating content: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, 0, 0, fmt.Errorf("empty response from model")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
	}

	var out llmVerdict
	if err := json.Unmarshal([]byte(llmutil.CleanJSON(text)), &out); err != nil {
		return nil, 0, 0, fmt.Errorf("parsing JSON (%q): %w", text, err)
	}

	var inTok, outTok int
	if resp.UsageMetadata != nil {
		inTok = int(resp.UsageMetadata.PromptTokenCount)
		outTok = int(resp.UsageMetadata.CandidatesTokenCount)
	}
	return &out, inTok, outTok, nil
}

// Review runs the deterministic and semantic gates against the current
// working tree in repoDir. It does not mutate repo content (though
// `git add -N .` is used to surface untracked files in the diff — this
// is a no-op on file contents and only touches the git index).
//
// Returns a Verdict describing the outcome. A non-nil error means the
// gate itself failed to run (not that the code was rejected) — callers
// should distinguish Verdict.Approved from err.
func Review(
	ctx context.Context,
	geminiKey string,
	repoDir string,
	issue *github.Issue,
	plan *planner.ImplementationPlan,
	dossier *archivist.Dossier,
) (*Verdict, error) {
	verdict := &Verdict{}

	// 1. go build
	build, err := RunBuild(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("running go build: %w", err)
	}
	verdict.BuildOutput = build.Output
	if !build.Passed {
		verdict.Approved = false
		verdict.Category = "build"
		verdict.Summary = "go build failed"
		verdict.Feedback = "## Build Failure\n\n" + build.Output
		return verdict, nil
	}

	// 2. go vet
	vet, err := RunVet(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("running go vet: %w", err)
	}
	verdict.VetOutput = vet.Output
	if !vet.Passed {
		verdict.Approved = false
		verdict.Category = "vet"
		verdict.Summary = "go vet failed"
		verdict.Feedback = "## Vet Failure\n\n" + vet.Output
		return verdict, nil
	}

	// 3. Collect the diff (including untracked files via `git add -N .`).
	diff, files, err := collectDiff(ctx, repoDir)
	if err != nil {
		return nil, fmt.Errorf("collecting diff: %w", err)
	}

	// 4. Semantic LLM review.
	prompt := buildReviewPrompt(issue, plan, dossier, diff, files)
	llmResult, inTokens, outTokens, err := reviewSemantics(ctx, geminiKey, prompt)
	if err != nil {
		return nil, fmt.Errorf("semantic reviewer: %w", err)
	}

	verdict.InputTokens = inTokens
	verdict.OutputTokens = outTokens
	verdict.CostUSD = cost.Calculate(geminiModel, inTokens, outTokens)
	verdict.SemanticResult = llmResult.Feedback

	if !llmResult.Approved {
		verdict.Approved = false
		verdict.Category = "semantic"
		verdict.Summary = "semantic review rejected"
		verdict.Feedback = "## Semantic Review Rejection\n\n" + llmResult.Feedback
		return verdict, nil
	}

	verdict.Approved = true
	verdict.Summary = llmResult.Feedback
	return verdict, nil
}

// collectDiff runs `git add -N .` followed by `git diff HEAD` to produce
// a unified diff that includes both modified and untracked files. It
// also returns the list of touched files parsed from `git status --porcelain`.
//
// All three git commands share a single gitTimeout bound so a stuck
// git index or hung subprocess cannot block the pipeline indefinitely.
func collectDiff(ctx context.Context, repoDir string) (string, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	addCmd := exec.CommandContext(ctx, "git", "add", "-N", ".")
	addCmd.Dir = repoDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("git add -N: %w\n%s", err, out)
	}

	diffCmd := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diffCmd.Dir = repoDir
	var diffOut bytes.Buffer
	diffCmd.Stdout = &diffOut
	var diffStderr bytes.Buffer
	diffCmd.Stderr = &diffStderr
	if err := diffCmd.Run(); err != nil {
		return "", nil, fmt.Errorf("git diff HEAD: %w\n%s", err, diffStderr.String())
	}
	diff := diffOut.String()
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n... (diff truncated)"
	}

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("git status: %w", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(statusOut), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			files = append(files, parts[len(parts)-1])
		}
	}
	return diff, files, nil
}
