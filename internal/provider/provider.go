package provider

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrTransient = errors.New("transient provider error")
	ErrPermanent = errors.New("permanent provider error")
)

// CompletionRequest is a provider-agnostic chat completion request.
type CompletionRequest struct {
	Model   string
	Prompt  string
	Timeout time.Duration
}

// Usage captures token accounting when the provider reports it.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

// CompletionResponse is a normalized provider response.
type CompletionResponse struct {
	Text     string
	Raw      json.RawMessage
	Usage    Usage
	Latency  time.Duration
	TimedOut bool
}

// Provider abstracts an LLM backend.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

// Registry maps provider name -> implementation.
type Registry map[string]Provider

func (r Registry) Get(name string) (Provider, bool) {
	p, ok := r[name]
	return p, ok
}

// IsTransient reports whether an error should be retried.
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient) || errors.Is(err, context.DeadlineExceeded)
}
