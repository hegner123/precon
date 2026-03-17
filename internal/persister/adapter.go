package persister

import (
	"context"

	"github.com/hegner123/precon/internal/api"
)

// ClientAdapter wraps an api.Client to implement the persister LLM interface.
// Bridges between the concrete API client and the consumer-defined interface.
type ClientAdapter struct {
	client *api.Client
	model  string
}

// NewClientAdapter creates an LLM adapter for the given API client and model.
func NewClientAdapter(client *api.Client, model string) *ClientAdapter {
	return &ClientAdapter{
		client: client,
		model:  model,
	}
}

// Complete implements LLM.Complete using a simple non-streaming API call.
func (a *ClientAdapter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	sys := api.NewSystemString(systemPrompt)
	req := &api.Request{
		Model:     a.model,
		Messages:  []api.MessageParam{api.NewUserMessage(userPrompt)},
		System:    &sys,
		MaxTokens: 2048,
	}

	resp, err := a.client.Send(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.GetText(), nil
}

// CompleteWithTools implements LLM.CompleteWithTools using a non-streaming API call.
// Returns the full response so the caller can inspect tool_use blocks.
func (a *ClientAdapter) CompleteWithTools(ctx context.Context, req *api.Request) (*api.Response, error) {
	return a.client.Send(ctx, req)
}
