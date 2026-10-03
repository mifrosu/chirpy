package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type apiHandler struct{}

type apiConfig struct {
	// safely write and read an int across go routines
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

// handlerMetrics reports the number of file server requests counted so far.
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Hits: %d\n", cfg.fileserverHits.Load())
}

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hits reset to 0\n"))
}

func (apiHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func handlerReadiness(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

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
	const staticPath = "./static"

	cfg := &apiConfig{}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler{})
	mux.HandleFunc("GET /api/healthz", handlerReadiness)
	mux.HandleFunc("GET /api/metrics", cfg.handlerMetrics)
	mux.HandleFunc("POST /api/reset", cfg.handlerReset)
	// Serve only ./static under /app/; FileServer serves static/app/index.html
	// for "/app/" and 404s anything that doesn't exist. Mounting at "/app/"
	// lets the mux redirect "/app" itself, so the redirect isn't counted by
	// the metrics middleware (which counts each request to this handler).
	mux.Handle("/app/", cfg.middlewareMetricsInc(http.FileServer(http.Dir(staticPath))))

	server := &http.Server{
		Addr:           ":" + port,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20, // bitwise left shift, 2 power 20 (1 MB)
	}

	slog.Info("Serving", "port", port, "path", staticPath)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
