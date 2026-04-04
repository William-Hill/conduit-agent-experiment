package reporting

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

var reportTmpl = template.Must(
	template.New("report").Funcs(template.FuncMap{
		"joinStrings": strings.Join,
	}).Parse(reportTemplate),
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
{{ if .Run.TriageDecision }}
## Triage

| Field | Value |
|-------|-------|
| Decision | {{ .Run.TriageDecision }} |
| Reason | {{ .Run.TriageReason }} |
{{ end }}
{{ if .Run.VerifierSummary }}
## Verification

**Result:** {{ .Run.VerifierSummary }}
{{ if .Run.CommandsRun }}
| Command | Exit Code |
|---------|-----------|
{{ range .Run.CommandsRun -}}
| ` + "`{{ .Command }}`" + ` | {{ .ExitCode }} |
{{ end }}
{{- end }}
{{- end }}

{{ if .Run.ImplementerPlan }}
## Patch Plan

{{ .Run.ImplementerPlan }}
{{ end }}
{{ if .Run.ImplementerDiff }}
## Diff

` + "```" + `
{{ .Run.ImplementerDiff }}
` + "```" + `
{{ end }}
{{ if .Run.ArchitectDecision }}
## Architect Review

| Field | Value |
|-------|-------|
| Decision | {{ .Run.ArchitectDecision }} |

{{ .Run.ArchitectReview }}
{{ end }}
{{ if .Run.PRURL }}
## Pull Request

{{ .Run.PRURL }}
{{ end }}
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
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, reportData{
		Run:     run,
		Dossier: dossier,
		Task:    task,
	}); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
