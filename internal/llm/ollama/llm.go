package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"cv-evaluator/internal/llm/prompts"
	"cv-evaluator/internal/telemetry"
)

// maxErrorBody caps how much of an error response is read.
const maxErrorBody = 64 << 10

type Service struct {
	baseURl string
	client  *http.Client
}

// New returns an Ollama service. Requests go through a copy of client whose
// transport is instrumented with OpenTelemetry (peer.service=ollama).
func New(client *http.Client, baseURL string) *Service {
	return &Service{
		client:  telemetry.NewHTTPClient(telemetry.PeerOllama, client),
		baseURl: baseURL,
	}
}

func (s *Service) CleanupCV(ctx context.Context, content string) (string, error) {
	promptName := "cv-cleanup.txt"
	b, err := s.loadPrompt(promptName)
	if err != nil {
		return "", err
	}
	prompt := string(b) + "\n\n" + prompts.TagCV(content)
	req := Request{
		Model: "qwen3:8b",
		//Model:  model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
	}

	res, err := s.callLLM(telemetry.WithOperation(ctx, telemetry.OpCVCleanup), "/api/generate", req)
	if err != nil {
		return "", err
	}

	var response *Response
	if err := json.NewDecoder(bytes.NewBuffer(res)).Decode(&response); err != nil {
		return "", err
	}

	return response.Response, nil
}

func (s *Service) CreateEmbedding(ctx context.Context, content string) ([]float32, error) {
	slog.DebugContext(ctx, "creating embedding with Ollama", "content_length", len(content))
	req := Request{
		Model:  "nomic-embed-text",
		Prompt: content,
	}

	res, err := s.callLLM(telemetry.WithOperation(ctx, telemetry.OpEmbedding), "/api/embeddings", req)
	if err != nil {
		return nil, err
	}

	response := struct {
		Embedding []float32 `json:"embedding"`
	}{}

	if err := json.NewDecoder(bytes.NewBuffer(res)).Decode(&response); err != nil {
		return nil, err
	}

	if len(response.Embedding) == 0 {
		return nil, errors.New("ollama embeddings: empty embedding in response")
	}

	return response.Embedding, nil
}

func (s *Service) loadPrompt(fileName string) ([]byte, error) {
	return prompts.Prompts.ReadFile(fileName)
}

func (s *Service) callLLM(ctx context.Context, url string, content Request) ([]byte, error) {
	slog.DebugContext(ctx, "calling ollama", "path", url, "model", content.Model, "prompt_length", len(content.Prompt))
	payload, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURl+url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		return nil, statusError(url, res)
	}

	return io.ReadAll(res.Body)
}

// statusError describes a non-200 response, using Ollama's error message
// when the body has one.
func statusError(url string, res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		return fmt.Errorf("ollama %s: status %d: %s", url, res.StatusCode, e.Error)
	}
	return fmt.Errorf("ollama %s: status %s", url, res.Status)
}
