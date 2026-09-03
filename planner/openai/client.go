package openai

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

// Client is a minimal OpenAI-compatible chat-completions client. It intentionally
// depends only on the wire contract so GamePilot can talk to hosted OpenAI APIs
// or local servers such as LM Studio, Ollama, and vLLM without an SDK dependency.
type Client struct {
	BaseURL    string
	Model      string
	APIKey     string
	HTTPClient *http.Client

	// Thinking controls Qwen/vLLM-style chat-template thinking. Nil omits the
	// provider-specific field entirely; false requests fast non-thinking mode;
	// true explicitly enables thinking.
	Thinking *bool

	// MaxTokens bounds the tiny placement response. Zero omits the field.
	MaxTokens int
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model              string         `json:"model"`
	Messages           []chatMessage  `json:"messages"`
	Temperature        float64        `json:"temperature"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// NewClient creates a client for an OpenAI-compatible /chat/completions API.
// baseURL should normally include /v1, for example http://localhost:1234/v1.
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:   strings.TrimSpace(model),
		APIKey:  strings.TrimSpace(apiKey),
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CompleteJSON asks the model for a JSON-only response. Schema enforcement is
// deliberately performed by the game-specific planner after this call because
// many local OpenAI-compatible servers do not implement Structured Outputs.
func (c *Client) CompleteJSON(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openai-compatible client is nil")
	}
	if c.BaseURL == "" {
		return "", fmt.Errorf("openai-compatible base URL is required")
	}
	if c.Model == "" {
		return "", fmt.Errorf("openai-compatible model is required")
	}
	if c.MaxTokens < 0 {
		return "", fmt.Errorf("openai-compatible max tokens cannot be negative")
	}

	request := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0,
		MaxTokens:   c.MaxTokens,
	}
	if c.Thinking != nil {
		request.ChatTemplateKwargs = map[string]any{"enable_thinking": *c.Thinking}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("marshal chat completion request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create chat completion request: %w", err)
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
		return "", fmt.Errorf("chat completion request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read chat completion response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("chat completion HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded chatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("decode chat completion response: %w", err)
	}
	if decoded.Error != nil {
		return "", fmt.Errorf("chat completion error: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("chat completion response has no choices")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("chat completion response has empty content")
	}
	return content, nil
}
