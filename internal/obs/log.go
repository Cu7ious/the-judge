package obs

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a JSON slog logger. Room to wrap with OpenTelemetry later.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}

// WithRunAttrs returns a logger enriched with common correlation fields.
// TODO(otel): attach these as span attributes when OpenTelemetry is added.
func WithRunAttrs(log *slog.Logger, runID, caseRunID, provider string, attempt int) *slog.Logger {
	return log.With(
		"run_id", runID,
		"case_run_id", caseRunID,
		"provider", provider,
		"attempt", attempt,
	)
}
