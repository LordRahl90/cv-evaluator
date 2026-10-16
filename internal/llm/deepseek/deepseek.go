package deepseek

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/llm/prompts"
	"cv-evaluator/internal/telemetry"

	"github.com/cohesion-org/deepseek-go"
	"go.opentelemetry.io/otel/attribute"
)

// Values of the llm.task attribute on DeepSeek requests.
const (
	taskCVCleanup = "cv_cleanup"
	taskJobMatch  = "job_match"
)

type Client interface {
	CreateChatCompletion(ctx context.Context, req *deepseek.ChatCompletionRequest) (*deepseek.ChatCompletionResponse, error)
}

type Service struct {
	client Client
}

// NewClient returns a DeepSeek API client whose HTTP requests are traced and
// recorded as external requests (peer.service=deepseek). It returns nil when
// apiKey is empty and DEEPSEEK_API_KEY is unset, like deepseek.NewClient.
func NewClient(apiKey string) *deepseek.Client {
	c := deepseek.NewClient(apiKey)
	if c != nil {
		c.HTTPClient = telemetry.NewHTTPClient(telemetry.PeerDeepSeek, nil)
	}
	return c
}

func New(client Client) *Service {
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
	return s.callLLM(ctx, taskCVCleanup, prompt)
}

func (s *Service) JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error) {
	slog.DebugContext(ctx, "Matching CV with job description using DeepSeek LLM", "job_description_length", len(matchInput.JobDescription))

	prompt, err := s.buildPrompt(matchInput)
	if err != nil {
		return "", err
	}

	return s.callLLM(ctx, taskJobMatch, prompt)
}

// callLLM sends one chat completion. task labels the request (llm.task) in
// the telemetry recorded by the client built with NewClient.
func (s *Service) callLLM(ctx context.Context, task, content string) (string, error) {
	ctx = telemetry.WithOperation(ctx, telemetry.OpChatCompletion, attribute.String(telemetry.AttrLLMTask, task))

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
