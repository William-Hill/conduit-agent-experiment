package agents

import (
	"context"
	"strings"
	"time"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/llm"
	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// cleanJSONResponse strips markdown fences and whitespace from LLM JSON responses.
func cleanJSONResponse(s string) string {
	cleaned := strings.TrimSpace(s)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}

// callLLM makes an LLM completion call and records timing/metadata in an LLMCall.
func callLLM(ctx context.Context, client *llm.Client, agentName, modelName, systemPrompt, userPrompt string) (string, models.LLMCall, error) {
	start := time.Now()
	response, err := client.Complete(ctx, systemPrompt, userPrompt)
	duration := time.Since(start)

	call := models.LLMCall{
		Agent:    agentName,
		Model:    modelName,
		Prompt:   userPrompt,
		Response: response,
		Duration: duration.String(),
	}

	return response, call, err
}
