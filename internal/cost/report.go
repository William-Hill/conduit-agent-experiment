package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

type CostReport struct {
	TotalCostUSD      float64    `json:"total_cost_usd"`
	TotalInputTokens  int        `json:"total_input_tokens"`
	TotalOutputTokens int        `json:"total_output_tokens"`
	Steps             []StepCost `json:"steps"`
	BudgetInfo        BudgetInfo `json:"budget"`
}

type StepCost struct {
	Step         string  `json:"step"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Calls        int     `json:"calls"`
}

type BudgetInfo struct {
	PipelineCapUSD float64 `json:"pipeline_cap_usd"`
	Exceeded       bool    `json:"exceeded"`
}

func WriteCostReport(dir string, calls []models.LLMCall, budget Budget) error {
	type stepAccum struct {
		model        string
		inputTokens  int
		outputTokens int
		calls        int
	}
	byStep := make(map[string]*stepAccum)
	var stepOrder []string

	for _, c := range calls {
		step := c.Agent
		if len(step) > 6 && step[len(step)-6:] == "-retry" {
			step = step[:len(step)-6]
		}

		acc, ok := byStep[step]
		if !ok {
			acc = &stepAccum{model: c.Model}
			byStep[step] = acc
			stepOrder = append(stepOrder, step)
		}
		acc.inputTokens += c.InputTokens
		acc.outputTokens += c.OutputTokens
		acc.calls++
	}

	var steps []StepCost
	var totalInput, totalOutput int
	for _, step := range stepOrder {
		acc := byStep[step]
		totalInput += acc.inputTokens
		totalOutput += acc.outputTokens
		steps = append(steps, StepCost{
			Step:         step,
			Model:        acc.model,
			InputTokens:  acc.inputTokens,
			OutputTokens: acc.outputTokens,
			CostUSD:      Calculate(acc.model, acc.inputTokens, acc.outputTokens),
			Calls:        acc.calls,
		})
	}

	totalCost := CalculateCalls(calls)
	exceeded := budget.PipelineCap > 0 && totalCost > budget.PipelineCap

	report := CostReport{
		TotalCostUSD:      totalCost,
		TotalInputTokens:  totalInput,
		TotalOutputTokens: totalOutput,
		Steps:             steps,
		BudgetInfo: BudgetInfo{
			PipelineCapUSD: budget.PipelineCap,
			Exceeded:       exceeded,
		},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling cost report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cost.json"), data, 0644); err != nil {
		return fmt.Errorf("writing cost.json: %w", err)
	}
	return nil
}

func TotalTokens(calls []models.LLMCall) int {
	var total int
	for _, c := range calls {
		total += c.InputTokens + c.OutputTokens
	}
	return total
}
