package deepseek

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/cohesion-org/deepseek-go"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	code := 1
	defer func() {
		os.Exit(code)
	}()

	code = m.Run()
}

func TestService_CallLLM(t *testing.T) {
	svc := New(&mockClient{
		chatCompletionFunc: func(ctx context.Context, req *deepseek.ChatCompletionRequest) (*deepseek.ChatCompletionResponse, error) {
			return &deepseek.ChatCompletionResponse{
				Choices: []deepseek.Choice{
					{
						Message: deepseek.Message{
							Content: `{"answer":"The sky is blue because of Rayleigh scattering."}`,
						},
					},
				},
			}, nil
		},
	})

	res, err := svc.callLLM(t.Context(), "why is the sky blue? list the response in json format")
	require.NoError(t, err)
	require.NotEmpty(t, res)

	callCount := httpmock.GetTotalCallCount()

	fmt.Printf("\n\nResponse is: %s\nTotal call count: %d\n\n", res, callCount)
}

type mockClient struct {
	chatCompletionFunc func(ctx context.Context, req *deepseek.ChatCompletionRequest) (*deepseek.ChatCompletionResponse, error)
}

func (m *mockClient) CreateChatCompletion(ctx context.Context, req *deepseek.ChatCompletionRequest) (*deepseek.ChatCompletionResponse, error) {
	if m.chatCompletionFunc != nil {
		return m.chatCompletionFunc(ctx, req)
	}
	return nil, fmt.Errorf("not implemented")
}
