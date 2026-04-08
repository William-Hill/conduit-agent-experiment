package implementer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/toolrunner"
)

// Input types for each tool.

// ReadFileInput is the input schema for the read_file tool.
type ReadFileInput struct {
	Path string `json:"path" jsonschema:"required,description=File path relative to the repository root"`
}

// WriteFileInput is the input schema for the write_file tool.
type WriteFileInput struct {
	Path    string `json:"path" jsonschema:"required,description=File path relative to the repository root"`
	Content string `json:"content" jsonschema:"required,description=Complete file content to write"`
}

// ListDirInput is the input schema for the list_dir tool.
type ListDirInput struct {
	Path string `json:"path" jsonschema:"description=Directory path relative to repo root. Empty or '.' for root."`
}

// SearchFilesInput is the input schema for the search_files tool.
type SearchFilesInput struct {
	Pattern string `json:"pattern" jsonschema:"required,description=Regex pattern to search for in file contents"`
	Path    string `json:"path" jsonschema:"description=Directory to search in relative to repo root. Empty for entire repo."`
	Glob    string `json:"glob" jsonschema:"description=File glob filter. Example: *.go"`
}

// RunCommandInput is the input schema for the run_command tool.
type RunCommandInput struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to execute in the repo directory"`
}

// textResult is a helper that wraps a string in the tool result union type.
func textResult(text string) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	return anthropic.BetaToolResultBlockParamContentUnion{
		OfText: &anthropic.BetaTextBlockParam{Text: text},
	}, nil
}

// safePath resolves a relative path within repoDir, rejecting directory traversal
// and symlink escapes. Uses filepath.Rel to avoid prefix-based bypass.
func safePath(repoDir, relPath string) (string, error) {
	if relPath == "" || relPath == "." {
		return repoDir, nil
	}
	full := filepath.Clean(filepath.Join(repoDir, relPath))
	cleanRoot := filepath.Clean(repoDir)

	// Ensure the cleaned path is within the repo root using a separator-aware check.
	if full != cleanRoot && !strings.HasPrefix(full, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes repository root", relPath)
	}

	// Double-check with filepath.Rel — reject any result starting with "..".
	rel, err := filepath.Rel(cleanRoot, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes repository root", relPath)
	}

	// Resolve symlinks and verify the real path is still inside the repo.
	realRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolving repo root: %w", err)
	}
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		// File may not exist yet (write_file) — fall back to parent dir check.
		parentDir := filepath.Dir(full)
		realParent, pErr := filepath.EvalSymlinks(parentDir)
		if pErr != nil {
			return full, nil // parent doesn't exist yet either, will be created
		}
		if !strings.HasPrefix(realParent, realRoot+string(filepath.Separator)) && realParent != realRoot {
			return "", fmt.Errorf("path %q escapes repository root via symlink", relPath)
		}
		return full, nil
	}
	if realFull != realRoot && !strings.HasPrefix(realFull, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes repository root via symlink", relPath)
	}

	return full, nil
}

// NewTools creates the five coding tools scoped to the given repo directory.
func NewTools(repoDir string) ([]anthropic.BetaTool, error) {
	repoDir = filepath.Clean(repoDir)

	readFile, err := toolrunner.NewBetaToolFromJSONSchema[ReadFileInput](
		"read_file",
		"Read the contents of a file in the repository.",
		func(ctx context.Context, input ReadFileInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			p, err := safePath(repoDir, input.Path)
			if err != nil {
				return textResult(fmt.Sprintf("Error: %v", err))
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return textResult(fmt.Sprintf("Error: %v", err))
			}
			return textResult(string(data))
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating read_file tool: %w", err)
	}

	writeFile, err := toolrunner.NewBetaToolFromJSONSchema[WriteFileInput](
		"write_file",
		"Create or overwrite a file in the repository. Creates parent directories as needed.",
		func(ctx context.Context, input WriteFileInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			p, err := safePath(repoDir, input.Path)
			if err != nil {
				return textResult(fmt.Sprintf("Error: %v", err))
			}
			dir := filepath.Dir(p)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return textResult(fmt.Sprintf("Error: creating directories: %v", err))
			}
			if err := os.WriteFile(p, []byte(input.Content), 0o644); err != nil {
				return textResult(fmt.Sprintf("Error: writing file: %v", err))
			}
			return textResult(fmt.Sprintf("Wrote %d bytes to %s", len(input.Content), input.Path))
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating write_file tool: %w", err)
	}

	listDir, err := toolrunner.NewBetaToolFromJSONSchema[ListDirInput](
		"list_dir",
		"List the contents of a directory. Directories are suffixed with /.",
		func(ctx context.Context, input ListDirInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			p, err := safePath(repoDir, input.Path)
			if err != nil {
				return textResult(fmt.Sprintf("Error: %v", err))
			}
			entries, err := os.ReadDir(p)
			if err != nil {
				return textResult(fmt.Sprintf("Error: %v", err))
			}
			var sb strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				sb.WriteString(name)
				sb.WriteString("\n")
			}
			return textResult(sb.String())
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating list_dir tool: %w", err)
	}

	searchFiles, err := toolrunner.NewBetaToolFromJSONSchema[SearchFilesInput](
		"search_files",
		"Search for a regex pattern in files using grep. Returns matching lines with file paths and line numbers.",
		func(ctx context.Context, input SearchFilesInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			searchDir := repoDir
			if input.Path != "" {
				var err error
				searchDir, err = safePath(repoDir, input.Path)
				if err != nil {
					return textResult(fmt.Sprintf("Error: %v", err))
				}
			}

			args := []string{"-r", "-n"}
			if input.Glob != "" {
				args = append(args, "--include="+input.Glob)
			}
			// Use -e to safely pass the pattern (prevents -prefixed patterns
			// from being interpreted as flags).
			args = append(args, "-e", input.Pattern, "--", searchDir)

			cmd := exec.CommandContext(ctx, "grep", args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				// grep returns exit code 1 when no matches found — not an error
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return textResult("No matches found.")
				}
				return textResult(fmt.Sprintf("Error: %v\n%s", err, stderr.String()))
			}

			// Make paths relative to repoDir for cleaner output
			output := strings.ReplaceAll(stdout.String(), repoDir+"/", "")
			const maxSearchOutput = 64 * 1024
			if len(output) > maxSearchOutput {
				output = output[:maxSearchOutput] + "\n... (truncated)"
			}
			return textResult(output)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating search_files tool: %w", err)
	}

	// allowedCommands maps the base command to permitted subcommands (empty slice = any subcommand).
	allowedCommands := map[string][]string{
		"go":   {"build", "test", "vet", "fmt", "mod"},
		"make": {},
		"git":  {"diff", "status", "log"},
	}

	runCommand, err := toolrunner.NewBetaToolFromJSONSchema[RunCommandInput](
		"run_command",
		"Execute an allowed command in the repository directory. Permitted: go (build/test/vet/fmt/mod), make, git (diff/status/log).",
		func(ctx context.Context, input RunCommandInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			argv := strings.Fields(input.Command)
			if len(argv) == 0 {
				return textResult("Error: empty command")
			}

			base := filepath.Base(argv[0])
			if argv[0] != base {
				return textResult(fmt.Sprintf("Error: command must be a bare name, not a path (%q)", argv[0]))
			}
			allowed, ok := allowedCommands[base]
			if !ok {
				return textResult(fmt.Sprintf("Error: command %q is not allowed. Permitted: go, make, git", base))
			}
			if len(allowed) > 0 && len(argv) > 1 {
				sub := argv[1]
				found := false
				for _, a := range allowed {
					if a == sub {
						found = true
						break
					}
				}
				if !found {
					return textResult(fmt.Sprintf("Error: %s %s is not allowed. Permitted subcommands: %v", base, sub, allowed))
				}
			}

			cmdCtx, cancel := context.WithTimeout(ctx, 120*1e9) // 2 minute timeout
			defer cancel()

			cmd := exec.CommandContext(cmdCtx, base, argv[1:]...)
			cmd.Dir = repoDir
			cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					return textResult(fmt.Sprintf("Error: %v", err))
				}
			}

			var sb strings.Builder
			if stdout.Len() > 0 {
				sb.WriteString(stdout.String())
			}
			if stderr.Len() > 0 {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString("stderr:\n")
				sb.WriteString(stderr.String())
			}
			sb.WriteString(fmt.Sprintf("\nexit_code: %d", exitCode))
			return textResult(sb.String())
		},
	)
	if err != nil {
		return nil, fmt.Errorf("creating run_command tool: %w", err)
	}

	return []anthropic.BetaTool{readFile, writeFile, listDir, searchFiles, runCommand}, nil
}
