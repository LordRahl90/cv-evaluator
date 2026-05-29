package deepseek

import (
	"bytes"
	"context"
	"cv-solution/internal/entities"
	"fmt"
	"html/template"
	"log/slog"

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
	slog.DebugContext(ctx, "Cleaning up CV content with DeepSeek LLM", "content_length", len(content))
	promptContent, err := s.loadPrompt("cv-cleanup.txt")
	if err != nil {
		return "", err
	}

	prompt := fmt.Sprintf("%s\n\n%s", string(promptContent), content)
	return s.callLLM(ctx, prompt)
}

func (s *Service) JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error) {
	slog.DebugContext(ctx, "Matching CV with job description using DeepSeek LLM", "job_description_length", len(matchInput.JobDescription))

	prompt, err := s.buildPrompt(matchInput)
	if err != nil {
		return "", err
	}

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

func (s *Service) buildPrompt(input *entities.MatchInput) (string, error) {
	promptContent, err := s.loadPrompt("jobmatch.tpl")
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("jobmatch").Parse(string(promptContent))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, input); err != nil {
		return "", err
	}

	return buf.String(), nil
}
