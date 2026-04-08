package llm

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Client wraps the OpenAI-compatible API for LLM completions.
type Client struct {
	client *openai.Client
	model  string
}

// NewClient creates an LLM client pointing at the given base URL.
func NewClient(baseURL, apiKey, model string) *Client {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &Client{client: &client, model: model}
}

// Complete sends a system+user prompt and returns the assistant response,
// along with input and output token counts.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, int, int, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(c.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("LLM completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("LLM returned no choices")
	}

	return resp.Choices[0].Message.Content,
		int(resp.Usage.PromptTokens),
		int(resp.Usage.CompletionTokens),
		nil
}
