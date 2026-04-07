// Package logging provides the agency structured logging factory.
//
// Call New() once at startup to get a configured *slog.Logger, then set it
// as the process-wide default with slog.SetDefault(). After that, any code
// can use slog.Info(), slog.Warn(), etc. with no imports beyond "log/slog".
//
// For request-scoped fields (correlation ID, agent name), use WithContext()
// in middleware to attach a child logger, and FromContext() in handlers to
// retrieve it.
package logging

import (
	"context"
	"log/slog"
	"os"

	"golang.org/x/term"
)

type ctxKey struct{}

// New creates a structured logger for the given component.
//
// Format is selected by AGENCY_LOG_FORMAT env var:
//   - "json" (default): JSON objects, one per line
//   - "text": human-readable key=value format
//
// If AGENCY_LOG_FORMAT is unset and stderr is a terminal, text mode is
// used automatically for developer convenience.
func New(component, buildID string) *slog.Logger {
	format := os.Getenv("AGENCY_LOG_FORMAT")
	if format == "" {
		if term.IsTerminal(int(os.Stderr.Fd())) {
			format = "text"
		} else {
			format = "json"
		}
	}

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler).With(
		"component", component,
		"build_id", buildID,
	)
}

// WithContext returns a new context with the given logger attached.
// Use this in middleware to propagate request-scoped fields.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext extracts the logger from the context. If none is set,
// returns slog.Default() so callers never get nil.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
