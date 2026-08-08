// Package logger provides a process-wide levelled logger backed by log/slog.
//
// Call Init once at program startup (before any goroutines log). All subsequent
// calls to slog.Debug/Info/Warn/Error, as well as the legacy log.Print* family,
// are routed through the same handler at the configured level.
//
// Level is controlled by the LOG_LEVEL environment variable:
//
//	debug   — every HTTP request, every DB operation, startup config dump
//	info    — startup banner, migrations, errors  (default)
//	warn    — warnings and errors only
//	error   — errors only
package logger

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"
)

// activeLevel holds the level chosen at Init time.
var activeLevel = slog.LevelInfo

// Init configures the global slog default and redirects the legacy log package
// to write through slog at INFO level. Call this once, as early as possible.
func Init(level string) {
	activeLevel = parseLevel(level)

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: activeLevel,
		// Add source location in debug mode.
		AddSource: activeLevel == slog.LevelDebug,
	})

	logger := slog.New(handler)

	// SetDefault also redirects log.Print* to slog at LevelInfo (Go 1.21+).
	slog.SetDefault(logger)

	// Silence Gin's own writer; our middleware handles request logging.
	log.SetFlags(0)
	log.SetPrefix("")

	slog.LogAttrs(context.Background(), slog.LevelInfo, "logger initialised",
		slog.String("level", activeLevel.String()),
	)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "d", "verbose", "trace":
		return slog.LevelDebug
	case "warn", "warning", "w":
		return slog.LevelWarn
	case "error", "err", "e":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
