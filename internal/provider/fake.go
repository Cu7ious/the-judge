package provider

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Fake is a test double that returns scripted responses.
type Fake struct {
	mu       sync.Mutex
	NameStr  string
	Response string
	Err      error
	Delay    time.Duration
	Calls    int
}

func (f *Fake) Name() string {
	if f.NameStr == "" {
		return "fake"
	}
	return f.NameStr
}

func (f *Fake) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	f.mu.Lock()
	f.Calls++
	f.mu.Unlock()

	start := time.Now()
	if f.Delay > 0 {
		select {
		case <-ctx.Done():
			return &CompletionResponse{TimedOut: true, Latency: time.Since(start)}, ctx.Err()
		case <-time.After(f.Delay):
		}
	}
	if err := ctx.Err(); err != nil {
		return &CompletionResponse{TimedOut: true, Latency: time.Since(start)}, err
	}
	if f.Err != nil {
		return nil, f.Err
	}

	raw, _ := json.Marshal(map[string]any{
		"model": req.Model,
		"text":  f.Response,
	})
	return &CompletionResponse{
		Text:    f.Response,
		Raw:     raw,
		Latency: time.Since(start),
		Usage:   Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}
