package archivist

type FileEntry struct {
	Path    string `json:"path"`
	Reason  string `json:"reason"`
	Content string `json:"content"`
}

type Dossier struct {
	Summary  string      `json:"summary"`
	Files    []FileEntry `json:"files"`
	Risks    []string    `json:"risks"`
	Approach string      `json:"approach"`
}
