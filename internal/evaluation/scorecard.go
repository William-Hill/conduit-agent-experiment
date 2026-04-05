package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Scorecard aggregates evaluation results across multiple runs.
type Scorecard struct {
	TotalRuns           int            `json:"total_runs"`
	SuccessfulRuns      int            `json:"successful_runs"`
	PRsCreated          int            `json:"prs_created"`
	AvgFilesChanged     float64        `json:"avg_files_changed"`
	AvgDiffLines        float64        `json:"avg_diff_lines"`
	AvgLLMCalls         float64        `json:"avg_llm_calls"` // also rendered as "Avg Iterations" in FormatScorecard
	SuccessByDifficulty map[string]int `json:"success_by_difficulty"`
	FailureModes        map[string]int `json:"failure_modes"`

	// Pass rates (denominator: TotalRuns).
	LintPassRate  float64 `json:"lint_pass_rate"`
	BuildPassRate float64 `json:"build_pass_rate"`
	TestsPassRate float64 `json:"tests_pass_rate"`

	// Rate versions of existing count maps.
	AcceptanceRateByDifficulty map[string]float64 `json:"acceptance_rate_by_difficulty"`
	RejectionRateByFailureMode map[string]float64 `json:"rejection_rate_by_failure_mode"`

	// Qualitative aggregation (populated only when any run has scores).
	QualitativeScoreCount int                `json:"qualitative_score_count"`
	AvgQualitativeScores  map[string]float64 `json:"avg_qualitative_scores"`
}

// GenerateScorecard reads all evaluation.json files from subdirectories of
// runsDir and aggregates the results into a Scorecard.
func GenerateScorecard(runsDir string) (Scorecard, error) {
	sc := Scorecard{
		SuccessByDifficulty:        make(map[string]int),
		FailureModes:               make(map[string]int),
		AcceptanceRateByDifficulty: make(map[string]float64),
		RejectionRateByFailureMode: make(map[string]float64),
		AvgQualitativeScores:       make(map[string]float64),
	}

	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return sc, fmt.Errorf("read runs dir %s: %w", runsDir, err)
	}

	var totalFiles, totalDiff, totalLLM int
	var lintPassCount, buildPassCount, testsPassCount int
	runsByDifficulty := make(map[string]int)
	qualSums := make(map[string]int)
	qualCounts := make(map[string]int)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		evalPath := filepath.Join(runsDir, entry.Name(), "evaluation.json")
		data, err := os.ReadFile(evalPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return sc, fmt.Errorf("read %s: %w", evalPath, err)
		}

		var ev models.Evaluation
		if err := json.Unmarshal(data, &ev); err != nil {
			return sc, fmt.Errorf("unmarshal %s: %w", evalPath, err)
		}

		sc.TotalRuns++
		totalFiles += ev.FilesChanged
		totalDiff += ev.DiffLines
		totalLLM += ev.LLMCalls

		if ev.LintPass {
			lintPassCount++
		}
		if ev.BuildPass {
			buildPassCount++
		}
		if ev.TestsPass {
			testsPassCount++
		}

		if ev.Difficulty != "" {
			runsByDifficulty[ev.Difficulty]++
		}

		successful := ev.ImplementerSuccess && ev.VerifierPass && ev.ArchitectDecision == "approve"
		if successful {
			sc.SuccessfulRuns++
			if ev.Difficulty != "" {
				sc.SuccessByDifficulty[ev.Difficulty]++
			}
		}

		if ev.PRCreated {
			sc.PRsCreated++
		}

		// Only count FailureMode for actually-failed runs. A stale failure_mode
		// on an approved run would otherwise inflate RejectionRateByFailureMode
		// beyond 1.0 (numerator includes the run, denominator totalFailed does not).
		if !successful && ev.FailureMode != "" {
			sc.FailureModes[string(ev.FailureMode)]++
		}

		qualFields := []struct {
			name  string
			value int
		}{
			{"architectural_alignment", ev.ArchitecturalAlignment},
			{"rationale_clarity", ev.RationaleClarity},
			{"retrieval_usefulness", ev.RetrievalUsefulness},
			{"reviewer_confidence", ev.ReviewerConfidence},
			{"patch_readability", ev.PatchReadability},
		}
		scoredThisRun := false
		for _, q := range qualFields {
			if q.value != 0 {
				if q.value < 1 || q.value > 5 {
					return sc, fmt.Errorf("qualitative field %s has invalid value %d (must be 0 or 1-5) in %s", q.name, q.value, evalPath)
				}
				qualSums[q.name] += q.value
				qualCounts[q.name]++
				scoredThisRun = true
			}
		}
		if scoredThisRun {
			sc.QualitativeScoreCount++
		}
	}

	if sc.TotalRuns > 0 {
		sc.AvgFilesChanged = float64(totalFiles) / float64(sc.TotalRuns)
		sc.AvgDiffLines = float64(totalDiff) / float64(sc.TotalRuns)
		sc.AvgLLMCalls = float64(totalLLM) / float64(sc.TotalRuns)
		sc.LintPassRate = float64(lintPassCount) / float64(sc.TotalRuns)
		sc.BuildPassRate = float64(buildPassCount) / float64(sc.TotalRuns)
		sc.TestsPassRate = float64(testsPassCount) / float64(sc.TotalRuns)
	}

	for diff, total := range runsByDifficulty {
		if total > 0 {
			sc.AcceptanceRateByDifficulty[diff] = float64(sc.SuccessByDifficulty[diff]) / float64(total)
		}
	}

	totalFailed := sc.TotalRuns - sc.SuccessfulRuns
	if totalFailed > 0 {
		for mode, count := range sc.FailureModes {
			sc.RejectionRateByFailureMode[mode] = float64(count) / float64(totalFailed)
		}
	}

	for metric, count := range qualCounts {
		if count > 0 {
			sc.AvgQualitativeScores[metric] = float64(qualSums[metric]) / float64(count)
		}
	}

	return sc, nil
}

// sortedKeys returns the keys of a string-keyed map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FormatScorecard returns a human-readable markdown table representation of sc.
func FormatScorecard(sc Scorecard) string {
	var sb strings.Builder

	sb.WriteString("# Scorecard\n\n")

	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Total Runs | %d |\n", sc.TotalRuns))
	sb.WriteString(fmt.Sprintf("| Successful Runs | %d |\n", sc.SuccessfulRuns))
	sb.WriteString(fmt.Sprintf("| PRs Created | %d |\n", sc.PRsCreated))
	sb.WriteString(fmt.Sprintf("| Avg Files Changed | %.2f |\n", sc.AvgFilesChanged))
	sb.WriteString(fmt.Sprintf("| Avg Diff Lines | %.2f |\n", sc.AvgDiffLines))
	sb.WriteString(fmt.Sprintf("| Avg LLM Calls | %.2f |\n", sc.AvgLLMCalls))
	sb.WriteString(fmt.Sprintf("| Avg Iterations | %.2f |\n", sc.AvgLLMCalls))

	if len(sc.SuccessByDifficulty) > 0 {
		sb.WriteString("\n## Success by Difficulty\n\n")
		sb.WriteString("| Difficulty | Successes |\n")
		sb.WriteString("|------------|----------|\n")
		for _, k := range sortedKeys(sc.SuccessByDifficulty) {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", k, sc.SuccessByDifficulty[k]))
		}
	}

	if len(sc.FailureModes) > 0 {
		sb.WriteString("\n## Failure Modes\n\n")
		sb.WriteString("| Failure Mode | Count |\n")
		sb.WriteString("|-------------|-------|\n")
		for _, k := range sortedKeys(sc.FailureModes) {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", k, sc.FailureModes[k]))
		}
	}

	if sc.TotalRuns > 0 {
		sb.WriteString("\n## Pass Rates\n\n")
		sb.WriteString("| Check | Rate |\n")
		sb.WriteString("|-------|------|\n")
		sb.WriteString(fmt.Sprintf("| Lint  | %.2f |\n", sc.LintPassRate))
		sb.WriteString(fmt.Sprintf("| Build | %.2f |\n", sc.BuildPassRate))
		sb.WriteString(fmt.Sprintf("| Tests | %.2f |\n", sc.TestsPassRate))
	}

	if len(sc.AcceptanceRateByDifficulty) > 0 || len(sc.RejectionRateByFailureMode) > 0 {
		sb.WriteString("\n## Acceptance & Rejection Rates\n\n")

		if len(sc.AcceptanceRateByDifficulty) > 0 {
			sb.WriteString("| Difficulty | Acceptance Rate |\n")
			sb.WriteString("|------------|-----------------|\n")
			for _, k := range sortedKeys(sc.AcceptanceRateByDifficulty) {
				sb.WriteString(fmt.Sprintf("| %s | %.2f |\n", k, sc.AcceptanceRateByDifficulty[k]))
			}
		}

		if len(sc.RejectionRateByFailureMode) > 0 {
			if len(sc.AcceptanceRateByDifficulty) > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("| Failure Mode | Rejection Rate |\n")
			sb.WriteString("|--------------|----------------|\n")
			for _, k := range sortedKeys(sc.RejectionRateByFailureMode) {
				sb.WriteString(fmt.Sprintf("| %s | %.2f |\n", k, sc.RejectionRateByFailureMode[k]))
			}
		}
	}

	if sc.QualitativeScoreCount > 0 && len(sc.AvgQualitativeScores) > 0 {
		sb.WriteString("\n## Qualitative Scores\n\n")
		sb.WriteString(fmt.Sprintf("(Scored runs: %d)\n\n", sc.QualitativeScoreCount))
		sb.WriteString("| Metric | Avg (1-5) |\n")
		sb.WriteString("|--------|-----------|\n")
		for _, k := range sortedKeys(sc.AvgQualitativeScores) {
			sb.WriteString(fmt.Sprintf("| %s | %.1f |\n", k, sc.AvgQualitativeScores[k]))
		}
	}

	return sb.String()
}