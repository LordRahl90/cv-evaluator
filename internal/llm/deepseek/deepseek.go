package deepseek

import (
	"context"
	"fmt"

	"cv-solution/internal/llm/prompts"

	"github.com/cohesion-org/deepseek-go"
)

type Service struct {
	client *deepseek.Client
}

func New(key string) *Service {
	client := deepseek.NewClient(key)
	return &Service{
		client: client,
	}
}

func (s *Service) CleanupCV(ctx context.Context, content string) (string, error) {
	promptContent, err := s.loadPrompt("cv-cleanup.txt")
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf("%s\n\n%s", string(promptContent), content)
	return s.callLLM(ctx, prompt)
}

func (s *Service) callLLM(ctx context.Context, content string) (string, error) {
	req := &deepseek.ChatCompletionRequest{
		Model: deepseek.DeepSeekChat,
		ResponseFormat: &deepseek.ResponseFormat{
			Type: "json_object",
		},
		Messages: []deepseek.ChatCompletionMessage{
			{
				Role:    deepseek.ChatMessageRoleUser,
				Content: content,
			},
		},
	}

	res, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}

	return res.Choices[0].Message.Content, nil
}

func (s *Service) loadPrompt(fileName string) ([]byte, error) {
	return prompts.Prompts.ReadFile(fileName)
}
