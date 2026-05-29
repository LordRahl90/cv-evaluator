package affinda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	baseURL = "https://api.affinda.com/v3"
)

type Service struct {
	client *http.Client
	token  string
}

func New(client *http.Client, token string) *Service {
	return &Service{
		client: client,
		token:  token,
	}
}

func (s *Service) UploadRawDocument(ctx context.Context, content string) (string, error) {
	path := "/documents/create_from_data"
	b, err := s.sendPostRequest(ctx, path, []byte(content))
	if err != nil {
		return "", err
	}

	var response DocumentResponse
	if err := json.Unmarshal(b, &response); err != nil {
		return "", err
	}

	fmt.Printf("Response is: ")

	return response.Meta.ID, nil
}

func (s *Service) sendPostRequest(ctx context.Context, url string, body []byte) ([]byte, error) {
	fullPath := fmt.Sprintf("%s%s", baseURL, url)
	payload := map[string]interface{}{
		"workspace": "1009890",
		"data":      body,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	fmt.Printf("content: \n%s\n", string(content))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullPath, bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Response is: %s\n", string(b))

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	return b, nil
}

//func (s *Service) sendGetRequest(ctx context.Context, url string) ([]byte, error) {
//	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
//	if err != nil {
//		return nil, err
//	}
//
//	req.Header.Set("Authorization", "Bearer "+s.token)
//
//	res, err := s.client.Do(req)
//	if err != nil {
//		return nil, err
//	}
//	defer res.Body.Close()
//	if res.StatusCode != http.StatusOK {
//		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
//	}
//
//	return io.ReadAll(res.Body)
//}
