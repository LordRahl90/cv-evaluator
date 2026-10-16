package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"cv-evaluator/internal/llm/prompts"
	"cv-evaluator/internal/telemetry"
)

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
	ctx, span := telemetry.Start(ctx, "ollama.Service.CleanupCV")
	defer span.End()
	promptName := "cv-cleanup.txt"
	b, err := s.loadPrompt(promptName)
	if err != nil {
		return "", span.RecordError(err)
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
		return "", err // recorded on the HTTP client span
	}

	var response *Response
	if err := json.NewDecoder(bytes.NewBuffer(res)).Decode(&response); err != nil {
		return "", span.RecordError(err)
	}

	return response.Response, nil
}

func (s *Service) CreateEmbedding(ctx context.Context, content string) ([]float32, error) {
	ctx, span := telemetry.Start(ctx, "ollama.Service.CreateEmbedding")
	defer span.End()
	slog.DebugContext(ctx, "Creating embedding with Ollama", "content", content)
	req := Request{
		Model:  "nomic-embed-text",
		Prompt: content,
	}

	res, err := s.callLLM(telemetry.WithOperation(ctx, telemetry.OpEmbedding), "/api/embeddings", req)
	if err != nil {
		return nil, err // recorded on the HTTP client span
	}

	response := struct {
		Embedding []float32 `json:"embedding"`
	}{}

	if err := json.NewDecoder(bytes.NewBuffer(res)).Decode(&response); err != nil {
		return nil, span.RecordError(err)
	}

	return response.Embedding, nil
}

func (s *Service) loadPrompt(fileName string) ([]byte, error) {
	return prompts.Prompts.ReadFile(fileName)
}

func (s *Service) callLLM(ctx context.Context, url string, content Request) ([]byte, error) {
	slog.DebugContext(ctx, "calling ollama", "content", content)
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

	return io.ReadAll(res.Body)
}
