// Package openai creates embeddings with the OpenAI embeddings API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"cv-evaluator/internal/telemetry"
)

const (
	// DefaultBaseURL is the OpenAI API root.
	DefaultBaseURL = "https://api.openai.com/v1"

	// EmbeddingModel is the model used for every embedding.
	EmbeddingModel = "text-embedding-3-small"
	// EmbeddingDimensions matches the vector(768) columns. The model returns
	// 1536 dimensions unless asked for fewer.
	EmbeddingDimensions = 768

	// MaxInputChars caps the text sent to the model, which accepts at most
	// 8192 tokens. Scraped job descriptions can be longer; the start of the
	// text carries most of the meaning, so the rest is dropped.
	MaxInputChars = 20_000

	// maxErrorBody caps how much of an error response is read.
	maxErrorBody = 64 << 10
)

// Service creates embeddings with OpenAI.
type Service struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New returns an OpenAI embeddings service. Requests go through a copy of
// client whose transport is instrumented with OpenTelemetry
// (peer.service=openai). An empty baseURL uses DefaultBaseURL.
func New(client *http.Client, apiKey, baseURL string) *Service {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Service{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  telemetry.NewHTTPClient(telemetry.PeerOpenAI, client),
	}
}

type embeddingRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// CreateEmbedding returns a 768-dimension embedding of content. Content
// longer than MaxInputChars is truncated first.
func (s *Service) CreateEmbedding(ctx context.Context, content string) ([]float32, error) {
	input := truncate(content, MaxInputChars)
	slog.DebugContext(ctx, "creating embedding with OpenAI", "content_length", len(content), "input_length", len(input))

	payload, err := json.Marshal(embeddingRequest{
		Model:      EmbeddingModel,
		Input:      input,
		Dimensions: EmbeddingDimensions,
	})
	if err != nil {
		return nil, err
	}

	ctx = telemetry.WithOperation(ctx, telemetry.OpEmbedding)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	res, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: %w", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		return nil, statusError(res)
	}

	var response embeddingResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("openai embeddings: decode response: %w", err)
	}
	if len(response.Data) != 1 {
		return nil, fmt.Errorf("openai embeddings: expected 1 embedding, got %d", len(response.Data))
	}
	embedding := response.Data[0].Embedding
	if len(embedding) != EmbeddingDimensions {
		return nil, fmt.Errorf("openai embeddings: expected %d dimensions, got %d", EmbeddingDimensions, len(embedding))
	}

	return embedding, nil
}

// statusError describes a non-200 response, using OpenAI's error message
// when the body has one.
func statusError(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	var e errorResponse
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return fmt.Errorf("openai embeddings: status %d: %s", res.StatusCode, e.Error.Message)
	}
	return errors.New("openai embeddings: status " + res.Status)
}

// truncate returns s cut to at most maxChars runes.
func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	n := 0
	for i := range s {
		if n == maxChars {
			return s[:i]
		}
		n++
	}
	return s
}
