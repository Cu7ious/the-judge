package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds process configuration loaded from environment variables.
type Config struct {
	HTTPAddr            string
	DatabaseURL         string
	RedisAddr           string
	LogLevel            string
	MigrationsDir       string
	WorkerConcurrency   int
	MaxRetries          int
	LMStudioBaseURL     string
	GeminiAPIKey        string
	GeminiBaseURL       string
	ShutdownTimeout     time.Duration
	DefaultCaseTimeout  time.Duration
}

// Load reads configuration from the environment with sensible defaults for local development.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://judge:judge@localhost:5432/judge?sslmode=disable"),
		RedisAddr:          getenv("REDIS_ADDR", "localhost:6379"),
		LogLevel:           getenv("LOG_LEVEL", "info"),
		MigrationsDir:      getenv("MIGRATIONS_DIR", "migrations"),
		WorkerConcurrency:  getenvInt("WORKER_CONCURRENCY", 4),
		MaxRetries:         getenvInt("MAX_RETRIES", 3),
		LMStudioBaseURL:    getenv("LMSTUDIO_BASE_URL", "http://localhost:1234/v1"),
		GeminiAPIKey:       os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:      getenv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		ShutdownTimeout:    getenvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		DefaultCaseTimeout: getenvDuration("DEFAULT_CASE_TIMEOUT", 30*time.Second),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RedisAddr == "" {
		return Config{}, fmt.Errorf("REDIS_ADDR is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
