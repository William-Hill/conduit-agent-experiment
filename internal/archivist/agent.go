package archivist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llmutil"
	"google.golang.org/genai"
)

const archivistSystemPrompt = `You are a research archivist. Given a GitHub issue and search results from the repository, identify the relevant files and suggest an approach to fix the issue.

Output ONLY valid JSON with this schema:
{
  "summary": "one paragraph about what the issue is and what code is involved",
  "relevant_files": [
    {"path": "relative/path/to/file.go", "reason": "why this file matters"}
  ],
  "risks": ["risk 1", "risk 2"],
  "approach": "concrete steps to fix the issue"
}

Rules:
- List 3-8 relevant files, ranked by importance.
- For each file, explain WHY it's relevant in one sentence.
- The approach should be specific and actionable.
- Be conservative with risks — only flag real concerns.`

// dossierResponse is the JSON structure returned by the LLM.
type dossierResponse struct {
	Summary       string         `json:"summary"`
	RelevantFiles []RelevantFile `json:"relevant_files"`
	Risks         []string       `json:"risks"`
	Approach      string         `json:"approach"`
}

// RelevantFile is a file path + reason from the LLM response.
type RelevantFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RunArchivist explores the repo and builds a dossier using a single Gemini call.
// It pre-gathers search results in Go, then asks Gemini to analyze them.
func RunArchivist(ctx context.Context, geminiKey, repoDir, outputDir, issueTitle, issueBody string) (*Dossier, error) {
	// Pre-gather context with targeted searches
	log.Printf("  [archivist] gathering repo context...")

	keywords := extractKeywords(issueTitle + " " + issueBody)
	var searchResults strings.Builder

	// Search for relevant code using issue keywords
	for _, kw := range keywords {
		out := grepRepo(ctx, repoDir, kw, "*.go")
		if out != "" {
			fmt.Fprintf(&searchResults, "### Search: %q (*.go)\n%s\n\n", kw, truncate(out, 5000))
		}
	}

	// Also search proto files if the issue mentions API/swagger/proto
	body := strings.ToLower(issueTitle + " " + issueBody)
	if strings.Contains(body, "swagger") || strings.Contains(body, "proto") || strings.Contains(body, "api") {
		for _, kw := range keywords[:min(3, len(keywords))] {
			out := grepRepo(ctx, repoDir, kw, "*.proto")
			if out != "" {
				fmt.Fprintf(&searchResults, "### Search: %q (*.proto)\n%s\n\n", kw, truncate(out, 3000))
			}
		}
	}

	// Get top-level directory listing
	dirOut := listTopLevel(ctx, repoDir)

	log.Printf("  [archivist] calling Gemini Flash for analysis...")

	// Single Gemini call to analyze
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiKey})
	if err != nil {
		return nil, fmt.Errorf("creating genai client: %w", err)
	}

	prompt := fmt.Sprintf(`## GitHub Issue: %s

%s

## Repository Structure
%s

## Search Results
%s

Analyze the search results and identify the files relevant to fixing this issue.`,
		issueTitle, issueBody, dirOut, searchResults.String())

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash",
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText(archivistSystemPrompt, "user"),
			Temperature:       ptr(float32(0.2)),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("gemini call failed: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("empty response from gemini")
	}

	// Extract text from response
	var responseText string
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			responseText += part.Text
		}
	}

	// Parse JSON response
	cleaned := llmutil.CleanJSON(responseText)
	var dr dossierResponse
	if err := json.Unmarshal([]byte(cleaned), &dr); err != nil {
		return nil, fmt.Errorf("parsing archivist response: %w\nraw: %s", err, truncate(responseText, 500))
	}

	log.Printf("  [archivist] found %d relevant files", len(dr.RelevantFiles))

	// Build dossier with actual file contents
	files := make([]FileEntry, 0, len(dr.RelevantFiles))
	for _, rf := range dr.RelevantFiles {
		content := readFileContent(repoDir, rf.Path)
		if content != "" {
			files = append(files, FileEntry{
				Path:    rf.Path,
				Reason:  rf.Reason,
				Content: content,
			})
		}
	}

	if len(dr.RelevantFiles) > 0 && len(files) == 0 {
		return nil, fmt.Errorf("archivist could not read any of the %d files returned by the model (repoDir: %s)", len(dr.RelevantFiles), repoDir)
	}

	dossier := Dossier{
		Summary:  dr.Summary,
		Files:    files,
		Risks:    dr.Risks,
		Approach: dr.Approach,
	}

	if _, err := SaveDossier(outputDir, dossier); err != nil {
		return nil, fmt.Errorf("saving dossier: %w", err)
	}

	return &dossier, nil
}

// extractKeywords pulls significant words from the issue text.
func extractKeywords(text string) []string {
	stop := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"be": true, "to": true, "of": true, "and": true, "in": true, "for": true,
		"on": true, "it": true, "that": true, "this": true, "with": true, "as": true,
		"not": true, "but": true, "or": true, "from": true, "by": true, "all": true,
		"should": true, "needs": true, "need": true, "we": true, "can": true,
		"have": true, "has": true, "had": true, "been": true, "will": true,
		"would": true, "could": true, "which": true, "when": true, "where": true,
		"what": true, "how": true, "do": true, "does": true, "did": true,
	}

	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
	})

	seen := map[string]bool{}
	var keywords []string
	for _, w := range words {
		if len(w) < 3 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
		if len(keywords) >= 8 {
			break
		}
	}
	return keywords
}

// grepRepo runs grep on the repo and returns output.
func grepRepo(ctx context.Context, repoDir, pattern, glob string) string {
	args := []string{"-rn", "--include=" + glob, "-l", "-e", pattern, "--", "."}
	cmd := exec.CommandContext(ctx, "grep", args...)
	cmd.Dir = repoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Exit code 1 = no matches (normal). Log other errors for debugging.
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			log.Printf("  [archivist] grep %v in %s failed: %v", args, repoDir, err)
		}
	}
	return out.String()
}

// listTopLevel returns a simple listing of top-level directories.
func listTopLevel(ctx context.Context, repoDir string) string {
	cmd := exec.CommandContext(ctx, "ls", "-1")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	return string(out)
}

// readFileContent reads up to 500 lines of a file from the repo.
// Returns empty string if the path escapes repoDir or the file can't be read.
func readFileContent(repoDir, relPath string) string {
	// Resolve symlinks on both paths to prevent symlink escapes.
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(repoDir))
	if err != nil {
		return ""
	}
	full := filepath.Clean(filepath.Join(repoDir, relPath))
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "" // file doesn't exist
	}
	rel, err := filepath.Rel(realRoot, realFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}

	f, err := os.Open(realFull)
	if err != nil {
		return ""
	}
	defer f.Close()

	var sb strings.Builder
	var buf []byte
	remaining := make([]byte, 0, 4096)
	lines := 0
	const maxLines = 500

	for lines < maxLines {
		tmp := make([]byte, 32*1024)
		n, readErr := f.Read(tmp)
		if n > 0 {
			buf = append(remaining, tmp[:n]...)
			remaining = remaining[:0]
			for i := 0; i < len(buf) && lines < maxLines; {
				nl := bytes.IndexByte(buf[i:], '\n')
				if nl < 0 {
					remaining = append(remaining[:0], buf[i:]...)
					break
				}
				sb.Write(buf[i : i+nl+1])
				lines++
				i += nl + 1
			}
		}
		if readErr != nil {
			// Write any remaining partial line.
			if len(remaining) > 0 && lines < maxLines {
				sb.Write(remaining)
			}
			break
		}
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n... (truncated)"
	}
	return s
}

func ptr[T any](v T) *T { return &v }
