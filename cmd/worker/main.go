package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cu7ious/the-judge/internal/config"
	"github.com/cu7ious/the-judge/internal/jobs"
	"github.com/cu7ious/the-judge/internal/obs"
	"github.com/cu7ious/the-judge/internal/provider"
	"github.com/cu7ious/the-judge/internal/provider/gemini"
	"github.com/cu7ious/the-judge/internal/provider/lmstudio"
	"github.com/cu7ious/the-judge/internal/runner"
	"github.com/cu7ious/the-judge/internal/store"
	"github.com/hibiken/asynq"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := obs.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	st := store.New(pool)
	registry := provider.Registry{
		"lmstudio": lmstudio.New(cfg.LMStudioBaseURL),
		"gemini":   gemini.New(cfg.GeminiAPIKey, cfg.GeminiBaseURL),
	}

	r := &runner.Runner{
		Store:     st,
		Providers: registry,
		Log:       log,
	}

	srv := asynq.NewServer(jobs.NewRedisOpt(cfg.RedisAddr), asynq.Config{
		Concurrency: cfg.WorkerConcurrency,
		Queues: map[string]int{
			jobs.QueueDefault: 10,
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Error("task failed", "type", task.Type(), "err", err)
		}),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeExecuteCaseRun, r.HandleExecuteCaseRun)

	go func() {
		log.Info("worker starting", "concurrency", cfg.WorkerConcurrency)
		if err := srv.Run(mux); err != nil {
			log.Error("worker stopped", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down worker")
	srv.Shutdown()
}
