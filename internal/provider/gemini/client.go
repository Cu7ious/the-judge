package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cu7ious/the-judge/internal/provider"
)

// Client talks to the Google Gemini generateContent REST API.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func New(apiKey, baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 0,
		},
	}
}

func (c *Client) Name() string { return "gemini" }

type generateRequest struct {
	Contents []content `json:"contents"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

func (c *Client) Complete(ctx context.Context, req provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("%w: GEMINI_API_KEY not configured", provider.ErrPermanent)
	}
	start := time.Now()

	body, err := json.Marshal(generateRequest{
		Contents: []content{{Parts: []part{{Text: req.Prompt}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal: %v", provider.ErrPermanent, err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		c.baseURL, url.PathEscape(req.Model), url.QueryEscape(c.apiKey))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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
	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("%w: rate limited: %s", provider.ErrTransient, truncate(raw, 200))
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: status %d: %s", provider.ErrPermanent, resp.StatusCode, truncate(raw, 200))
	}

	var parsed generateResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", provider.ErrPermanent, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("%w: %s", provider.ErrPermanent, parsed.Error.Message)
	}

	var text strings.Builder
	if len(parsed.Candidates) > 0 {
		for _, p := range parsed.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
	}

	return &provider.CompletionResponse{
		Text: text.String(),
		Raw:  raw,
		Usage: provider.Usage{
			PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
			CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      parsed.UsageMetadata.TotalTokenCount,
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
