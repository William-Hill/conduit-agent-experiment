package reporting

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

const reportTemplate = `# Run Report: {{ .Run.ID }}

## Task

| Field | Value |
|-------|-------|
| ID | {{ .Task.ID }} |
| Title | {{ .Task.Title }} |
| Difficulty | {{ .Task.Difficulty }} |
| Blast Radius | {{ .Task.BlastRadius }} |

{{ .Task.Description }}

## Dossier

**Summary:** {{ .Dossier.Summary }}

### Related Files
{{ range .Dossier.RelatedFiles }}
- {{ . }}
{{- end }}

### Related Docs
{{ range .Dossier.RelatedDocs }}
- {{ . }}
{{- end }}

## Likely Commands
{{ range .Dossier.LikelyCommands }}
- ` + "`{{ . }}`" + `
{{- end }}

## Risks
{{ range .Dossier.Risks }}
- {{ . }}
{{- end }}

## Open Questions
{{ range .Dossier.OpenQuestions }}
- {{ . }}
{{- end }}

## Run Details

| Field | Value |
|-------|-------|
| Started | {{ .Run.StartedAt.Format "2006-01-02 15:04:05 UTC" }} |
| Ended | {{ .Run.EndedAt.Format "2006-01-02 15:04:05 UTC" }} |
| Status | {{ .Run.FinalStatus }} |
| Human Decision | {{ .Run.HumanDecision }} |
| Agents | {{ joinStrings .Run.AgentsInvoked ", " }} |
`

type reportData struct {
	Run     models.Run
	Dossier models.Dossier
	Task    models.Task
}

// RenderMarkdown produces a markdown report for a completed run.
func RenderMarkdown(run models.Run, dossier models.Dossier, task models.Task) (string, error) {
	funcMap := template.FuncMap{
		"joinStrings": joinStrings,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, reportData{
		Run:     run,
		Dossier: dossier,
		Task:    task,
	}); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
