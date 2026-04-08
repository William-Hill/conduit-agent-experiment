package archivist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func SaveDossier(dir string, d Dossier) (string, error) {
	path := filepath.Join(dir, "dossier.json")
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling dossier: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing dossier: %w", err)
	}
	return path, nil
}

func LoadDossier(path string) (*Dossier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading dossier: %w", err)
	}
	var d Dossier
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parsing dossier: %w", err)
	}
	return &d, nil
}
