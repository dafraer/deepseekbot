// Package deepseek provides a minimal client for the DeepSeek chat completion API.
// See https://api-docs.deepseek.com/ for the API reference.
package deepseek

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

const (
	// DefaultBaseURL is the public DeepSeek API endpoint.
	DefaultBaseURL = "https://api.deepseek.com"
	// DefaultModel is the general-purpose DeepSeek chat model.
	DefaultModel = "deepseek-v4-flash"

	chatCompletionsPath = "/chat/completions"
)

// Client talks to the DeepSeek chat completion API.
type Client struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	model      string
}

// New creates a DeepSeek API client. baseURL and model fall back to
// DefaultBaseURL and DefaultModel when empty.
func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
	}
}

// Message is a single chat message in the DeepSeek conversation format.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// Model returns the client's default model name.
func (c *Client) Model() string {
	return c.model
}

// Chat sends the user's message to DeepSeek using the given model and returns
// the assistant's reply. An empty model falls back to the client's default.
func (c *Client) Chat(ctx context.Context, model, userMessage string) (string, error) {
	if model == "" {
		model = c.model
	}
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []Message{
			{Role: "user", Content: userMessage},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call deepseek api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("deepseek api error (status %d): %s", resp.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("deepseek api error: unexpected status %d", resp.StatusCode)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("deepseek api returned no choices")
	}

	reply := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if reply == "" {
		return "", fmt.Errorf("deepseek api returned an empty reply")
	}
	return reply, nil
}
