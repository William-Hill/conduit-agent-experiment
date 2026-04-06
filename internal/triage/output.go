package triage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveRanking writes the triage output to a dated JSON file and returns the path.
func SaveRanking(dir string, output TriageOutput) (string, error) {
	date := output.Timestamp
	if i := strings.IndexByte(date, 'T'); i > 0 {
		date = date[:i]
	}

	filename := fmt.Sprintf("triage-%s.json", date)
	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling triage output: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing triage output: %w", err)
	}

	return path, nil
}
