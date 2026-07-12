package observability

import (
	"log/slog"
	"os"
)

// NewLogger builds the process's slog.Logger with a consistent handler
// configuration (JSON output on stdout, level configurable), so every
// package constructs loggers the same way instead of ad hoc
// log/slog.New(...) calls scattered around the codebase.
func NewLogger(level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}
