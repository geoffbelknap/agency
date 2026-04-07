package main

import (
	"log/slog"
	"os"
)

// initLogging sets up the process-wide slog default for the enforcer.
// After this call, slog.Info(), slog.Warn(), etc. emit structured JSON
// (or text if AGENCY_LOG_FORMAT=text).
func initLogging() {
	format := os.Getenv("AGENCY_LOG_FORMAT")
	if format == "" {
		format = "json"
	}

	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler).With(
		"component", "enforcer",
		"build_id", buildID,
	))
}
