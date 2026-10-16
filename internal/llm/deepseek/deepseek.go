package deepseek

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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

// Sampling temperatures. Low values keep scores and parsing stable across
// runs. They must stay above zero: the client omits a zero temperature
// (omitempty), and DeepSeek then falls back to its default of 1.0.
const (
	jobMatchTemperature  float32 = 0.2
	cvCleanupTemperature float32 = 0.1
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

	return s.callLLM(ctx, taskCVCleanup, cvCleanupTemperature, string(promptContent), prompts.TagCV(content))
}

func (s *Service) JobMatch(ctx context.Context, matchInput *entities.MatchInput) (string, error) {
	slog.DebugContext(ctx, "Matching CV with job description using DeepSeek LLM", "job_description_length", len(matchInput.JobDescription))

	system, user, err := s.buildPrompt(matchInput)
	if err != nil {
		return "", err
	}

	return s.callLLM(ctx, taskJobMatch, jobMatchTemperature, system, user)
}

// callLLM sends one chat completion: the instructions as the system message
// and the data as the user message. task labels the request (llm.task) in
// the telemetry recorded by the client built with NewClient.
func (s *Service) callLLM(ctx context.Context, task string, temperature float32, system, user string) (string, error) {
	ctx = telemetry.WithOperation(ctx, telemetry.OpChatCompletion, attribute.String(telemetry.AttrLLMTask, task))

	req := &deepseek.ChatCompletionRequest{
		Model:       deepseek.DeepSeekChat,
		Temperature: temperature,
		ResponseFormat: &deepseek.ResponseFormat{
			Type: "json_object",
		},
		Messages: []deepseek.ChatCompletionMessage{
			{Role: deepseek.ChatMessageRoleSystem, Content: system},
			{Role: deepseek.ChatMessageRoleUser, Content: user},
		},
	}

	res, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	if len(res.Choices) == 0 {
		return "", errors.New("deepseek: response has no choices")
	}

	return res.Choices[0].Message.Content, nil
}

func (s *Service) loadPrompt(fileName string) ([]byte, error) {
	return prompts.Prompts.ReadFile(fileName)
}

// buildPrompt renders the match prompt's "system" (instructions and
// output schema) and "user" (the job and CV) templates.
func (s *Service) buildPrompt(input *entities.MatchInput) (system, user string, err error) {
	promptContent, err := s.loadPrompt("jobmatch.tpl")
	if err != nil {
		return "", "", err
	}
	tmpl, err := template.New("jobmatch").Parse(string(promptContent))
	if err != nil {
		return "", "", err
	}

	render := func(name string) (string, error) {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, input); err != nil {
			return "", err
		}
		return strings.TrimSpace(buf.String()), nil
	}
	if system, err = render("system"); err != nil {
		return "", "", err
	}
	if user, err = render("user"); err != nil {
		return "", "", err
	}
	return system, user, nil
}
