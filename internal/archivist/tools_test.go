package archivist

import (
	"testing"
)

func TestNewToolsCount(t *testing.T) {
	tools, err := NewTools(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewTools: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(tools))
	}

	wantNames := []string{"read_file", "search_files", "save_dossier"}
	for i, name := range wantNames {
		if tools[i].Name() != name {
			t.Errorf("tools[%d].Name() = %q, want %q", i, tools[i].Name(), name)
		}
	}
}
