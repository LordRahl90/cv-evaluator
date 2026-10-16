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
	// taskCoverLetter labels cover letter requests, retries included.
	taskCoverLetter = "cover_letter"
)

// Sampling temperatures. Low values keep scores and parsing stable across
// runs. They must stay above zero: the client omits a zero temperature
// (omitempty), and DeepSeek then falls back to its default of 1.0.
const (
	jobMatchTemperature  float32 = 0.2
	cvCleanupTemperature float32 = 0.1
	// coverLetterTemperature is a little higher: the letter is prose, and
	// the server-side checks catch anything that strays from the rules.
	coverLetterTemperature float32 = 0.4
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

// CoverLetter writes a cover letter and returns the LLM's raw JSON answer
// ({"subject": ..., "letter": ...}). When input.Revision is set, the
// previous answer and the reasons it was rejected follow the first
// exchange, so the LLM revises its own letter.
func (s *Service) CoverLetter(ctx context.Context, input *entities.CoverLetterInput) (string, error) {
	slog.DebugContext(ctx, "Writing a cover letter using DeepSeek LLM", "job_description_length", len(input.JobDescription), "revision", input.Revision != nil)

	messages, err := s.buildCoverLetterMessages(input)
	if err != nil {
		return "", err
	}
	return s.complete(ctx, taskCoverLetter, coverLetterTemperature, messages)
}

// callLLM sends one chat completion: the instructions as the system message
// and the data as the user message. task labels the request (llm.task) in
// the telemetry recorded by the client built with NewClient.
func (s *Service) callLLM(ctx context.Context, task string, temperature float32, system, user string) (string, error) {
	return s.complete(ctx, task, temperature, []deepseek.ChatCompletionMessage{
		{Role: deepseek.ChatMessageRoleSystem, Content: system},
		{Role: deepseek.ChatMessageRoleUser, Content: user},
	})
}

// complete sends messages as one JSON-mode chat completion.
func (s *Service) complete(ctx context.Context, task string, temperature float32, messages []deepseek.ChatCompletionMessage) (string, error) {
	ctx = telemetry.WithOperation(ctx, telemetry.OpChatCompletion, attribute.String(telemetry.AttrLLMTask, task))

	req := &deepseek.ChatCompletionRequest{
		Model:       deepseek.DeepSeekChat,
		Temperature: temperature,
		ResponseFormat: &deepseek.ResponseFormat{
			Type: "json_object",
		},
		Messages: messages,
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
	render, err := s.templateRenderer("jobmatch.tpl", input)
	if err != nil {
		return "", "", err
	}
	if system, err = render("system"); err != nil {
		return "", "", err
	}
	if user, err = render("user"); err != nil {
		return "", "", err
	}
	return system, user, nil
}

// buildCoverLetterMessages renders the cover letter prompt: the rules as
// the system message and the candidate's name, job, CV and evaluation as
// the user message. On a revision the previous answer follows as the
// assistant's message, then the checks it failed as a second user message.
func (s *Service) buildCoverLetterMessages(input *entities.CoverLetterInput) ([]deepseek.ChatCompletionMessage, error) {
	render, err := s.templateRenderer("coverletter.tpl", input)
	if err != nil {
		return nil, err
	}
	system, err := render("system")
	if err != nil {
		return nil, err
	}
	user, err := render("user")
	if err != nil {
		return nil, err
	}
	messages := []deepseek.ChatCompletionMessage{
		{Role: deepseek.ChatMessageRoleSystem, Content: system},
		{Role: deepseek.ChatMessageRoleUser, Content: user},
	}
	if input.Revision == nil {
		return messages, nil
	}
	feedback, err := render("revision")
	if err != nil {
		return nil, err
	}
	return append(messages,
		deepseek.ChatCompletionMessage{Role: deepseek.ChatMessageRoleAssistant, Content: input.Revision.Previous},
		deepseek.ChatCompletionMessage{Role: deepseek.ChatMessageRoleUser, Content: feedback},
	), nil
}

// templateRenderer parses the prompt template fileName and returns a
// function that renders one of its named templates with data, trimmed.
func (s *Service) templateRenderer(fileName string, data any) (func(name string) (string, error), error) {
	promptContent, err := s.loadPrompt(fileName)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(fileName).Parse(string(promptContent))
	if err != nil {
		return nil, err
	}
	return func(name string) (string, error) {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return strings.TrimSpace(buf.String()), nil
	}, nil
}
