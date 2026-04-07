package archivist

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// Tool input/output types.

type ReadFileInput struct {
	Path string `json:"path"`
}

type ReadFileOutput struct {
	Content string `json:"content"`
}

type ListDirInput struct {
	Path string `json:"path"`
}

type ListDirOutput struct {
	Entries string `json:"entries"`
}

type SearchFilesInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
}

type SearchFilesOutput struct {
	Matches string `json:"matches"`
}

type SaveDossierInput struct {
	Summary       string         `json:"summary"`
	RelevantFiles []RelevantFile `json:"relevant_files"`
	Risks         []string       `json:"risks"`
	Approach      string         `json:"approach"`
}

type RelevantFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type SaveDossierOutput struct {
	FileCount int    `json:"file_count"`
	Path      string `json:"path"`
}

// safePath resolves relPath under baseDir and ensures it doesn't escape.
func safePath(baseDir, relPath string) (string, error) {
	abs := filepath.Join(baseDir, filepath.Clean(relPath))
	if !strings.HasPrefix(abs, filepath.Clean(baseDir)+string(filepath.Separator)) && abs != filepath.Clean(baseDir) {
		return "", fmt.Errorf("path %q escapes base directory", relPath)
	}
	return abs, nil
}

// NewTools creates the four function tools for the archivist agent.
func NewTools(repoDir, outputDir string) ([]tool.Tool, error) {
	readFile, err := functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Read the contents of a file in the repository. Provide a path relative to the repo root.",
	}, func(_ tool.Context, input ReadFileInput) (ReadFileOutput, error) {
		p, err := safePath(repoDir, input.Path)
		if err != nil {
			return ReadFileOutput{}, err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return ReadFileOutput{}, fmt.Errorf("reading file: %w", err)
		}
		return ReadFileOutput{Content: string(data)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating read_file tool: %w", err)
	}

	searchFiles, err := functiontool.New(functiontool.Config{
		Name:        "search_files",
		Description: "Search for a regex pattern in files using grep. Optionally restrict to a subdirectory (path) and/or a file glob pattern. Returns matching lines with file paths and line numbers.",
	}, func(ctx tool.Context, input SearchFilesInput) (SearchFilesOutput, error) {
		dir := repoDir
		if input.Path != "" {
			p, err := safePath(repoDir, input.Path)
			if err != nil {
				return SearchFilesOutput{}, err
			}
			dir = p
		}

		args := []string{"-rn"}
		if input.Glob != "" {
			args = append(args, "--include="+input.Glob)
		}
		args = append(args, input.Pattern, dir)

		cmd := exec.CommandContext(ctx, "grep", args...)
		out, err := cmd.Output()
		if err != nil {
			// grep returns exit code 1 when no matches found — not an error.
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return SearchFilesOutput{Matches: ""}, nil
			}
			return SearchFilesOutput{}, fmt.Errorf("grep: %w", err)
		}
		// Trim output to a reasonable size.
		result := string(out)
		if len(result) > 50000 {
			result = result[:50000] + "\n... (truncated)"
		}
		return SearchFilesOutput{Matches: result}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating search_files tool: %w", err)
	}

	saveDossier, err := functiontool.New(functiontool.Config{
		Name:        "save_dossier",
		Description: "Save your research findings as a structured dossier. Provide a summary, list of relevant files (each with path and reason), risks, and a suggested approach. File contents are read automatically. You MUST call this tool before finishing.",
	}, func(_ tool.Context, input SaveDossierInput) (SaveDossierOutput, error) {
		files := make([]FileEntry, 0, len(input.RelevantFiles))
		for _, rf := range input.RelevantFiles {
			p, err := safePath(repoDir, rf.Path)
			if err != nil {
				return SaveDossierOutput{}, err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return SaveDossierOutput{}, fmt.Errorf("reading %s: %w", rf.Path, err)
			}
			files = append(files, FileEntry{
				Path:    rf.Path,
				Reason:  rf.Reason,
				Content: string(data),
			})
		}

		dossier := Dossier{
			Summary:  input.Summary,
			Files:    files,
			Risks:    input.Risks,
			Approach: input.Approach,
		}

		path, err := SaveDossier(outputDir, dossier)
		if err != nil {
			return SaveDossierOutput{}, err
		}

		return SaveDossierOutput{
			FileCount: len(files),
			Path:      path,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("creating save_dossier tool: %w", err)
	}

	return []tool.Tool{readFile, searchFiles, saveDossier}, nil
}
