package main

import (
	"log/slog"
	"os"
)

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
		"component", "web-fetch",
		"build_id", buildID,
	))
}
