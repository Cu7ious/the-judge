package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cu7ious/the-judge/internal/api"
	"github.com/cu7ious/the-judge/internal/config"
	"github.com/cu7ious/the-judge/internal/jobs"
	"github.com/cu7ious/the-judge/internal/migrate"
	"github.com/cu7ious/the-judge/internal/obs"
	"github.com/cu7ious/the-judge/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := obs.NewLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runMigrations(ctx, cfg.DatabaseURL, cfg.MigrationsDir); err != nil {
		log.Error("migrations failed", "err", err)
		os.Exit(1)
	}

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	st := store.New(pool)
	enq := jobs.NewEnqueuer(cfg.RedisAddr, cfg.MaxRetries)
	defer enq.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	srv := api.NewServer(st, enq, rdb, log)
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: srv.Router(),
	}

	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down api")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func runMigrations(ctx context.Context, databaseURL, dir string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return migrate.Up(ctx, db, dir)
}
