package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"cv-evaluator/internal/llm/prompts"
)

type Service struct {
	baseURl string
	client  *http.Client
}

func New(client *http.Client, baseURL string) *Service {
	return &Service{
		client:  client,
		baseURl: baseURL,
	}
}

func (s *Service) CleanupCV(ctx context.Context, content string) (string, error) {
	promptName := "cv-cleanup.txt"
	b, err := s.loadPrompt(promptName)
	if err != nil {
		return "", err
	}
	prompt := fmt.Sprintf("%s\n\n%s", string(b), content)
	req := Request{
		Model: "qwen3:8b",
		//Model:  model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
	}

	res, err := s.callLLM(ctx, "/api/generate", req)
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
	slog.DebugContext(ctx, "Creating embedding with Ollama", "content", content)
	req := Request{
		Model:  "nomic-embed-text",
		Prompt: content,
	}

	res, err := s.callLLM(ctx, "/api/embeddings", req)
	if err != nil {
		return nil, err
	}

	response := struct {
		Embedding []float32 `json:"embedding"`
	}{}

	if err := json.NewDecoder(bytes.NewBuffer(res)).Decode(&response); err != nil {
		return nil, err
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
