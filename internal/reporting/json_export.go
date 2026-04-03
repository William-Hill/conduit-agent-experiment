package reporting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// WriteRunJSON writes the run record as JSON to the given directory.
func WriteRunJSON(dir string, run models.Run) error {
	return writeJSON(filepath.Join(dir, "run.json"), run)
}

// WriteDossierJSON writes the dossier as JSON to the given directory.
func WriteDossierJSON(dir string, dossier models.Dossier) error {
	return writeJSON(filepath.Join(dir, "dossier.json"), dossier)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
