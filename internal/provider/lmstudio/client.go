package lmstudio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cu7ious/the-judge/internal/provider"
)

// Client talks to LM Studio's OpenAI-compatible API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 0, // rely on request context
		},
	}
}

func (c *Client) Name() string { return "lmstudio" }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) Complete(ctx context.Context, req provider.CompletionRequest) (*provider.CompletionResponse, error) {
	start := time.Now()

	body, err := json.Marshal(chatRequest{
		Model: req.Model,
		Messages: []chatMessage{
			{Role: "user", Content: req.Prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", provider.ErrPermanent, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", provider.ErrPermanent, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return &provider.CompletionResponse{TimedOut: true, Latency: time.Since(start)}, fmt.Errorf("%w: %v", provider.ErrTransient, ctx.Err())
		}
		return nil, fmt.Errorf("%w: %v", provider.ErrTransient, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", provider.ErrTransient, err)
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: status %d: %s", provider.ErrTransient, resp.StatusCode, truncate(raw, 200))
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: status %d: %s", provider.ErrPermanent, resp.StatusCode, truncate(raw, 200))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", provider.ErrPermanent, err)
	}
	text := ""
	if len(parsed.Choices) > 0 {
		text = parsed.Choices[0].Message.Content
	}

	return &provider.CompletionResponse{
		Text: text,
		Raw:  raw,
		Usage: provider.Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
		Latency: time.Since(start),
	}, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
