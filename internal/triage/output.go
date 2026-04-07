package triage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveRanking writes the triage output to a dated JSON file and returns the path.
// Uses atomic write (temp file + rename) to avoid partial files on crash.
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

	tmp, err := os.CreateTemp(dir, ".triage-*.json")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("writing triage output: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("renaming triage output: %w", err)
	}

	return path, nil
}
