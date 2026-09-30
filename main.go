package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

type apiHandler struct{}

func (apiHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func main() {
	// TextHandler emits RFC 3339 (ISO 8601) timestamps; convert them to UTC.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	const port = "8080"

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler{})
	// Serve only ./static; FileServer serves index.html for "/" and 404s
	// anything that doesn't exist.
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	server := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20, // bitwise left shift, 2 power 20 (1 MB)
	}

	slog.Info("Serving", "port", port)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
