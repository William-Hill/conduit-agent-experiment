package models

// Dossier is the context package assembled by the Archivist agent
// before a task is attempted.
type Dossier struct {
	TaskID        string   `json:"task_id"`
	Summary       string   `json:"summary"`
	RelatedFiles  []string `json:"related_files"`
	RelatedDocs   []string `json:"related_docs"`
	LikelyCommands []string `json:"likely_commands"`
	Risks         []string `json:"risks"`
	OpenQuestions []string `json:"open_questions"`
}
