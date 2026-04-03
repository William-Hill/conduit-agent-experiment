package ingest

import (
	"os"
	"path/filepath"
	"strings"
)

// FileCategory classifies a file's role in the repository.
type FileCategory string

const (
	CategoryCode     FileCategory = "code"
	CategoryTest     FileCategory = "test"
	CategoryDocs     FileCategory = "docs"
	CategoryADR      FileCategory = "adr"
	CategoryWorkflow FileCategory = "workflow"
	CategoryConfig   FileCategory = "config"
)

// FileEntry represents a single file in the inventory.
type FileEntry struct {
	Path     string       `json:"path"`     // relative to repo root
	Category FileCategory `json:"category"`
	Size     int64        `json:"size"`
}

// FileInventory is the classified file listing for a repository.
type FileInventory struct {
	RepoRoot string      `json:"repo_root"`
	Files    []FileEntry `json:"files"`
}

// FilesByCategory returns all files matching the given category.
func (inv *FileInventory) FilesByCategory(cat FileCategory) []FileEntry {
	var out []FileEntry
	for _, f := range inv.Files {
		if f.Category == cat {
			out = append(out, f)
		}
	}
	return out
}

// skipDir returns true for directories that should be excluded from inventory.
func skipDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "bin", "dist":
		return true
	}
	return false
}

// ClassifyFile determines the category of a file based on its path.
func ClassifyFile(relPath string) FileCategory {
	base := filepath.Base(relPath)
	dir := filepath.Dir(relPath)
	ext := filepath.Ext(base)

	// Tests: any file ending in _test.go or under a test/ directory.
	if strings.HasSuffix(base, "_test.go") {
		return CategoryTest
	}
	if strings.Contains(relPath, "/test/") || strings.HasPrefix(relPath, "test/") {
		return CategoryTest
	}

	// Workflows: anything under .github/workflows.
	if strings.HasPrefix(relPath, ".github/workflows") {
		return CategoryWorkflow
	}

	// ADRs: files under docs/design-documents, docs/adr, or named adr-*.
	if strings.Contains(dir, "design-documents") || strings.Contains(dir, "docs/adr") {
		return CategoryADR
	}

	// Docs: markdown files in docs/, README, CONTRIBUTING, CHANGELOG, etc.
	if strings.HasPrefix(relPath, "docs/") && (ext == ".md" || ext == ".txt" || ext == ".rst") {
		return CategoryDocs
	}
	upperBase := strings.ToUpper(strings.TrimSuffix(base, ext))
	switch upperBase {
	case "README", "CONTRIBUTING", "CHANGELOG", "LICENSE":
		return CategoryDocs
	}

	// Config: yaml, toml, json config files, Makefile, Dockerfile, go.mod, go.sum.
	switch base {
	case "Makefile", "Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"go.mod", "go.sum", ".goreleaser.yml", ".golangci.yml", ".golangci.yaml":
		return CategoryConfig
	}
	switch ext {
	case ".yaml", ".yml", ".toml", ".ini":
		if !strings.HasPrefix(relPath, "docs/") {
			return CategoryConfig
		}
	}

	// Code: everything else with a code extension.
	switch ext {
	case ".go", ".py", ".js", ".ts", ".rs", ".java", ".c", ".h", ".cpp", ".proto", ".sh":
		return CategoryCode
	}

	// Fallback: treat as code if no extension matched.
	return CategoryCode
}

// WalkRepo walks the directory tree at root and returns a classified FileInventory.
func WalkRepo(root string) (*FileInventory, error) {
	inv := &FileInventory{RepoRoot: root}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories.
		if info.IsDir() {
			if skipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Skip hidden files (except .github).
		if strings.HasPrefix(relPath, ".") && !strings.HasPrefix(relPath, ".github") {
			return nil
		}

		inv.Files = append(inv.Files, FileEntry{
			Path:     relPath,
			Category: ClassifyFile(relPath),
			Size:     info.Size(),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return inv, nil
}
