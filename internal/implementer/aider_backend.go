package implementer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Compile-time assertion that AiderBackend satisfies the Backend interface.
var _ Backend = (*AiderBackend)(nil)

// AiderBackend drives the `aider` CLI (https://aider.chat) as the implementer.
// The spec is written to a temp file and passed via --message-file. Target
// files from the archivist's dossier are passed as positional args so aider
// scopes its edits to them. Cost/token parsing comes from aider's stdout
// "Tokens:" line.
type AiderBackend struct {
	openrouterKey string
	model         string // e.g. "openrouter/qwen/qwen3-coder:free"
	aiderPath     string // path to aider binary; "aider" for PATH lookup
}

// NewAiderBackend constructs a backend that shells out to the aider CLI.
// aiderPath of "" defaults to "aider" (resolved via PATH).
func NewAiderBackend(openrouterKey, model, aiderPath string) *AiderBackend {
	if aiderPath == "" {
		aiderPath = "aider"
	}
	if model == "" {
		// qwen3-coder:free has 262K context (vs. 32K on the older
		// 2.5-coder-32b) which is needed to fit our planner's long
		// narrative markdown plus aider's repo map. Confirmed live on
		// the OpenRouter /models API as of the issue #38 smoke test.
		model = "openrouter/qwen/qwen3-coder:free"
	}
	return &AiderBackend{
		openrouterKey: openrouterKey,
		model:         model,
		aiderPath:     aiderPath,
	}
}

// Name returns "aider:<model>" for run-summary partitioning.
func (b *AiderBackend) Name() string {
	return "aider:" + b.model
}

// Run writes the plan to a temp file, invokes aider non-interactively, and
// parses aider's stdout for token counts. Iterations is always 1 because
// aider runs as a single invocation (aider manages its own internal retries).
func (b *AiderBackend) Run(ctx context.Context, params RunParams) (*Result, error) {
	if params.Plan == nil {
		return nil, fmt.Errorf("aider backend: nil plan")
	}

	specFile, err := os.CreateTemp("", "aider-spec-*.md")
	if err != nil {
		return nil, fmt.Errorf("create spec file: %w", err)
	}
	defer os.Remove(specFile.Name())
	if _, err := specFile.WriteString(params.Plan.Markdown); err != nil {
		specFile.Close()
		return nil, fmt.Errorf("write spec file: %w", err)
	}
	specFile.Close()

	args := []string{
		"--message-file", specFile.Name(),
		"--yes",
		"--auto-commits",
		"--no-pretty",
		"--no-stream",
		"--disable-playwright", // Aider scrapes URLs from messages by default;
		// our planner emits URL-heavy markdown which
		// blows past free-tier context limits.
		"--model", b.model,
	}
	// Resolve target file paths against RepoDir so aider can find them.
	for _, f := range params.TargetFiles {
		args = append(args, filepath.Join(params.RepoDir, f))
	}

	cmd := exec.CommandContext(ctx, b.aiderPath, args...)
	cmd.Dir = params.RepoDir
	cmd.Env = append(os.Environ(),
		"OPENROUTER_API_KEY="+b.openrouterKey,
		"AIDER_ANALYTICS=false",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		return nil, fmt.Errorf("aider run failed: %w: %s", runErr, truncateStderr(stderr.String()))
	}

	out := stdout.String()
	inputTokens, outputTokens := 0, 0
	// Aider prints per-turn token lines and a cumulative session total at
	// the end — scan to the last match so we capture the session total,
	// not a mid-run sub-total.
	for _, line := range strings.Split(out, "\n") {
		if in, o, ok := parseAiderTokens(line); ok {
			inputTokens, outputTokens = in, o
		}
	}

	return &Result{
		ModelName:    b.model,
		Summary:      extractAiderSummary(out),
		Iterations:   1,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// aiderTokenRe matches: "Tokens: 12.3k sent, 1.5k received. Cost: ..."
var aiderTokenRe = regexp.MustCompile(`Tokens:\s+([0-9.]+[kM]?)\s+sent,\s+([0-9.]+[kM]?)\s+received`)

// parseAiderTokens extracts (input, output) token counts from a single line
// of aider stdout. Returns ok=false if the line does not match.
func parseAiderTokens(line string) (int, int, bool) {
	m := aiderTokenRe.FindStringSubmatch(line)
	if len(m) != 3 {
		return 0, 0, false
	}
	in, ok1 := parseAiderNumber(m[1])
	out, ok2 := parseAiderNumber(m[2])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return in, out, true
}

// parseAiderNumber parses tokens like "12.3k", "1.5M", "500" into an int.
func parseAiderNumber(s string) (int, bool) {
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1_000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "M"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "M")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int(f * mult), true
}

// extractAiderSummary grabs the last commit line from aider stdout as a
// short run summary. Returns the last 2KB of output if no commit line found.
func extractAiderSummary(out string) string {
	var lastCommit string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Commit ") {
			lastCommit = strings.TrimSpace(line)
		}
	}
	if lastCommit != "" {
		return lastCommit
	}
	if len(out) > 2048 {
		return out[len(out)-2048:]
	}
	return out
}

// maxStderrBytes caps the stderr size included in error messages to keep
// logs bounded and to reduce the blast radius if aider (or a downstream
// provider) echoes partial credentials on auth failure.
const maxStderrBytes = 4096

// truncateStderr returns the last maxStderrBytes of s with a marker prefix
// when truncation occurred. Returns s unchanged if already small.
func truncateStderr(s string) string {
	if len(s) <= maxStderrBytes {
		return s
	}
	return "...(truncated)..." + s[len(s)-maxStderrBytes:]
}
