// Command ab-analyze reads run-summary.json files from a directory tree and
// prints a comparison report partitioned by the `backend` field. Used by
// issue #38's implementer A/B experiment.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
)

type runSummary struct {
	Backend             string  `json:"backend"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	Iterations          int     `json:"iterations"`
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	BudgetExceeded      bool    `json:"budget_exceeded"`
	HallucinatedSymbols int     `json:"hallucinated_symbols"`
	Error               string  `json:"error"`
}

// ArmStats aggregates metrics across all runs for a given backend.
type ArmStats struct {
	Backend          string
	Runs             int
	MeanCost         float64
	MeanIterations   float64
	MeanInputTokens  float64
	MeanOutputTokens float64
	MeanHallucinated float64
	SuccessRate      float64
}

// Report is the analyzed result across all arms.
type Report struct {
	Arms []*ArmStats
}

// Arm returns the arm with the given backend name or nil if not found.
func (r *Report) Arm(backend string) *ArmStats {
	for _, a := range r.Arms {
		if a.Backend == backend {
			return a
		}
	}
	return nil
}

func analyze(root string) (*Report, error) {
	byBackend := map[string][]runSummary{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "run-summary.json" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var rs runSummary
		if err := json.Unmarshal(body, &rs); err != nil {
			log.Printf("skip %s: %v", path, err)
			return nil
		}
		if rs.Error != "" {
			rs.BudgetExceeded = true // treat errors as failures for success rate
		}
		byBackend[rs.Backend] = append(byBackend[rs.Backend], rs)
		return nil
	})
	if err != nil {
		return nil, err
	}

	report := &Report{}
	for backend, runs := range byBackend {
		stats := &ArmStats{Backend: backend, Runs: len(runs)}
		var successes int
		for _, r := range runs {
			stats.MeanCost += r.EstimatedCostUSD
			stats.MeanIterations += float64(r.Iterations)
			stats.MeanInputTokens += float64(r.InputTokens)
			stats.MeanOutputTokens += float64(r.OutputTokens)
			stats.MeanHallucinated += float64(r.HallucinatedSymbols)
			if !r.BudgetExceeded && r.Error == "" {
				successes++
			}
		}
		n := float64(stats.Runs)
		stats.MeanCost /= n
		stats.MeanIterations /= n
		stats.MeanInputTokens /= n
		stats.MeanOutputTokens /= n
		stats.MeanHallucinated /= n
		stats.SuccessRate = float64(successes) / n
		report.Arms = append(report.Arms, stats)
	}
	sort.Slice(report.Arms, func(i, j int) bool { return report.Arms[i].Backend < report.Arms[j].Backend })
	return report, nil
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: ab-analyze <runs-root>")
		os.Exit(2)
	}
	report, err := analyze(flag.Arg(0))
	if err != nil {
		log.Fatalf("analyze: %v", err)
	}
	fmt.Printf("%-60s  %6s  %10s  %6s  %8s  %8s  %6s  %10s\n",
		"BACKEND", "RUNS", "SUCCESS%", "ITERS", "IN TOK", "OUT TOK", "HALLU", "MEAN COST")
	for _, a := range report.Arms {
		fmt.Printf("%-60s  %6d  %9.1f%%  %6.1f  %8.0f  %8.0f  %6.1f  $%9.4f\n",
			a.Backend, a.Runs, a.SuccessRate*100,
			a.MeanIterations, a.MeanInputTokens, a.MeanOutputTokens,
			a.MeanHallucinated, a.MeanCost)
	}
}
