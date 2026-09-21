package ramclassify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenJevClient talks directly to the Jev-compatible POST /v1/systemone wire API.
type OpenJevClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type systemOneRequest struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type systemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func NewOpenJevClient(baseURL, apiKey string) *OpenJevClient {
	return &OpenJevClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *OpenJevClient) Decide(ctx context.Context, model string, state any, questions map[string]Question) (DecisionResponse, error) {
	if c == nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: OpenJev client is nil")
	}
	if c.BaseURL == "" {
		return DecisionResponse{}, fmt.Errorf("ramclassify: OpenJev base URL is required")
	}
	if strings.TrimSpace(model) == "" {
		return DecisionResponse{}, fmt.Errorf("ramclassify: OpenJev model is required")
	}
	if len(questions) == 0 {
		return DecisionResponse{}, fmt.Errorf("ramclassify: at least one OpenJev question is required")
	}

	body, err := json.Marshal(systemOneRequest{State: state, Model: model, Questions: questions})
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: marshal OpenJev request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/systemone", bytes.NewReader(body))
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: create OpenJev request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: OpenJev request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: read OpenJev response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DecisionResponse{}, fmt.Errorf("ramclassify: OpenJev HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var decoded systemOneResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return DecisionResponse{}, fmt.Errorf("ramclassify: decode OpenJev response: %w", err)
	}
	return DecisionResponse{Model: decoded.Model, Answers: decoded.Answers}, nil
}
