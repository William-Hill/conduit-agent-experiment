package cost

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

type Budget struct {
	PipelineCap float64
	StepCaps    map[string]float64
}

func LoadBudget() Budget {
	b := Budget{
		StepCaps: make(map[string]float64),
	}
	b.PipelineCap = EnvFloat("PIPELINE_MAX_COST")
	if v := EnvFloat("ARCHIVIST_MAX_COST"); v > 0 {
		b.StepCaps["archivist"] = v
	}
	if v := EnvFloat("PLANNER_MAX_COST"); v > 0 {
		b.StepCaps["planner"] = v
	}
	if v := EnvFloat("IMPL_MAX_COST"); v > 0 {
		b.StepCaps["implementer"] = v
	}
	return b
}

func (b Budget) CheckStep(step string, calls []models.LLMCall) error {
	cap, ok := b.StepCaps[step]
	if !ok || cap <= 0 {
		return nil
	}
	var stepCost float64
	for _, c := range calls {
		agent := strings.TrimSuffix(c.Agent, "-retry")
		if agent == step {
			stepCost += Calculate(c.Model, c.InputTokens, c.OutputTokens)
		}
	}
	if stepCost > cap {
		return fmt.Errorf("budget exceeded for step %q: $%.4f > cap $%.4f", step, stepCost, cap)
	}
	return nil
}

func (b Budget) CheckTotal(calls []models.LLMCall) error {
	if b.PipelineCap <= 0 {
		return nil
	}
	total := CalculateCalls(calls)
	if total > b.PipelineCap {
		return fmt.Errorf("pipeline budget exceeded: $%.4f > cap $%.4f", total, b.PipelineCap)
	}
	return nil
}

// EnvFloat reads a float64 from the named environment variable.
// Returns 0 if the variable is unset or not a valid float.
func EnvFloat(key string) float64 {
	s := os.Getenv(key)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
