package retrieval

import (
	"fmt"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/ingest"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "in": true, "of": true,
	"to": true, "and": true, "for": true, "is": true, "it": true,
	"that": true, "this": true, "with": true, "from": true, "or": true,
	"be": true, "are": true, "was": true, "were": true, "been": true,
	"has": true, "have": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "can": true, "not": true, "no": true, "any": true,
	"all": true, "each": true, "but": true, "if": true, "by": true,
	"on": true, "at": true, "up": true, "so": true, "as": true,
}

// BuildDossier assembles a Dossier for the given task using the file inventory
// and keyword-based search. This is the phase-1 retrieval strategy (no embeddings).
func BuildDossier(task models.Task, inv *ingest.FileInventory) models.Dossier {
	keywords := extractKeywords(task)

	pathResults := SearchByKeyword(inv, keywords)
	contentResults := SearchByContent(inv, keywords)

	seen := make(map[string]bool)
	var relatedFiles []string
	var relatedDocs []string

	addResult := func(r SearchResult) {
		if seen[r.Path] {
			return
		}
		seen[r.Path] = true
		switch r.Category {
		case ingest.CategoryDocs, ingest.CategoryADR:
			relatedDocs = append(relatedDocs, r.Path)
		default:
			relatedFiles = append(relatedFiles, r.Path)
		}
	}

	for _, r := range pathResults {
		addResult(r)
	}
	for _, r := range contentResults {
		addResult(r)
	}

	for _, f := range inv.FilesByCategory(ingest.CategoryADR) {
		if !seen[f.Path] {
			relatedDocs = append(relatedDocs, f.Path)
			seen[f.Path] = true
		}
	}

	commands := determineLikelyCommands(inv)
	risks := determineRisks(task, relatedFiles)

	return models.Dossier{
		TaskID:         task.ID,
		Summary:        fmt.Sprintf("Task %q targets: %s", task.Title, task.Description),
		RelatedFiles:   relatedFiles,
		RelatedDocs:    relatedDocs,
		LikelyCommands: commands,
		Risks:          risks,
		OpenQuestions:  []string{"Are the identified files the complete set of affected files?"},
	}
}

func extractKeywords(task models.Task) []string {
	seen := make(map[string]bool)
	var keywords []string

	for _, label := range task.Labels {
		lower := strings.ToLower(label)
		if !seen[lower] && len(lower) > 2 {
			seen[lower] = true
			keywords = append(keywords, lower)
		}
	}

	for _, text := range []string{task.Title, task.Description} {
		words := strings.Fields(strings.ToLower(text))
		for _, w := range words {
			w = strings.Trim(w, ".,;:!?\"'()[]{}")
			if len(w) > 2 && !stopWords[w] && !seen[w] {
				seen[w] = true
				keywords = append(keywords, w)
			}
		}
	}

	return keywords
}

func determineLikelyCommands(inv *ingest.FileInventory) []string {
	var commands []string

	hasMakefile := false
	for _, f := range inv.FilesByCategory(ingest.CategoryConfig) {
		if f.Path == "Makefile" {
			hasMakefile = true
			break
		}
	}

	if hasMakefile {
		commands = append(commands, "make test")
	} else {
		commands = append(commands, "go test ./...")
	}

	hasLintConfig := false
	for _, f := range inv.Files {
		if strings.Contains(f.Path, "golangci") {
			hasLintConfig = true
			break
		}
	}
	if hasLintConfig {
		commands = append(commands, "golangci-lint run ./...")
	}

	commands = append(commands, "go build ./...")

	return commands
}

func determineRisks(task models.Task, files []string) []string {
	var risks []string

	for _, f := range files {
		if strings.Contains(f, "runtime") || strings.Contains(f, "pipeline") {
			risks = append(risks, fmt.Sprintf("File %s may be in a critical path", f))
			break
		}
	}

	if task.BlastRadius != models.BlastRadiusLow {
		risks = append(risks, fmt.Sprintf("Blast radius is %s, not low", task.BlastRadius))
	}

	if len(risks) == 0 {
		risks = append(risks, "No major risks identified for this task scope")
	}

	return risks
}
