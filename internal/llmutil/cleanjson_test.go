package llmutil

import "testing"

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "markdown json fence",
			input: "```json\n{\"summary\":\"test\"}\n```",
			want:  `{"summary":"test"}`,
		},
		{
			name:  "plain json",
			input: `{"summary":"test"}`,
			want:  `{"summary":"test"}`,
		},
		{
			name:  "whitespace padded",
			input: "  \n{\"summary\":\"test\"}\n  ",
			want:  `{"summary":"test"}`,
		},
		{
			name:  "fence no language",
			input: "```\n{\"approved\":true}\n```",
			want:  `{"approved":true}`,
		},
		{
			name:  "single-line fenced json",
			input: "```json{\"ok\":true}```",
			want:  `{"ok":true}`,
		},
		{
			name:  "json containing backticks in string",
			input: `{"code":"use ` + "```" + `go for Go"}`,
			want:  `{"code":"use ` + "```" + `go for Go"}`,
		},
		{
			name:  "incomplete fence not stripped",
			input: "```json\n{\"partial\":true}",
			want:  "```json\n{\"partial\":true}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanJSON(tt.input)
			if got != tt.want {
				t.Errorf("CleanJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}
