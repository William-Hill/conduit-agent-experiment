package retrieval

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
)

// SearchResult represents a file that matched a search query.
type SearchResult struct {
	Path     string             `json:"path"`
	Category ingest.FileCategory `json:"category"`
	Score    int                `json:"score"` // number of keyword matches
}

// SearchByKeyword returns files whose path contains any of the given keywords.
// Results are sorted by score (number of keywords matched) descending.
func SearchByKeyword(inv *ingest.FileInventory, keywords []string) []SearchResult {
	var results []SearchResult
	for _, f := range inv.Files {
		score := 0
		lower := strings.ToLower(f.Path)
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			results = append(results, SearchResult{
				Path:     f.Path,
				Category: f.Category,
				Score:    score,
			})
		}
	}
	sortByScore(results)
	return results
}

// SearchByContent reads file contents and returns files containing any keyword.
// Only searches files under a size limit (1MB) to avoid reading large binaries.
func SearchByContent(inv *ingest.FileInventory, keywords []string) []SearchResult {
	const maxSize = 1 << 20 // 1MB

	var results []SearchResult
	for _, f := range inv.Files {
		if f.Size > maxSize {
			continue
		}
		fullPath := filepath.Join(inv.RepoRoot, f.Path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		content := strings.ToLower(string(data))
		score := 0
		for _, kw := range keywords {
			if strings.Contains(content, strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			results = append(results, SearchResult{
				Path:     f.Path,
				Category: f.Category,
				Score:    score,
			})
		}
	}
	sortByScore(results)
	return results
}

// sortByScore sorts results by score descending using insertion sort
// (adequate for the small result sets expected here).
func sortByScore(results []SearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
