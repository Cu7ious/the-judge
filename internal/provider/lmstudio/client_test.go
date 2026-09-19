package lmstudio_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cu7ious/the-judge/internal/provider"
	"github.com/cu7ious/the-judge/internal/provider/lmstudio"
)

func TestCompleteHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "Paris"}},
			},
			"usage": map[string]int{
				"prompt_tokens": 3, "completion_tokens": 1, "total_tokens": 4,
			},
		})
	}))
	defer srv.Close()

	client := lmstudio.New(srv.URL, "")
	resp, err := client.Complete(context.Background(), provider.CompletionRequest{
		Model:  "test",
		Prompt: "capital?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Paris" {
		t.Fatalf("text=%q", resp.Text)
	}
	if resp.Usage.TotalTokens != 4 {
		t.Fatalf("usage=%+v", resp.Usage)
	}
}

func TestCompleteSendsAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer lmstudio-test-key" {
			t.Fatalf("Authorization=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	client := lmstudio.New(srv.URL, "lmstudio-test-key")
	_, err := client.Complete(context.Background(), provider.CompletionRequest{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompleteTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	client := lmstudio.New(srv.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, provider.CompletionRequest{Model: "m", Prompt: "p"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !provider.IsTransient(err) {
		t.Fatalf("expected transient, got %v", err)
	}
}
