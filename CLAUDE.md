# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Chirpy is a small Go web server (stdlib only, no dependencies) built as a Boot.dev backend learning project. All server code lives in `main.go`; tests are in `main_test.go`. `go-notes.md` holds the author's personal Go learning notes (not project docs).

## Commands

- Run: `go run .` (serves on `:8080`; must be run from the repo root since the static path is `./static`)
- Build: `go build -o chirpy .` (the `/chirpy` binary is gitignored)
- Test all: `go test ./...`
- Single test: `go test -run TestHandlerReset ./...`

## Architecture

Routing is a single `http.ServeMux` built in `main()` using Go 1.22+ method-qualified patterns (e.g. `"GET /api/healthz"`):

- `/app/` — `http.FileServer` over `./static`, wrapped in `cfg.middlewareMetricsInc`. Files live under `static/app/` (so `static/app/index.html` is served at `/app/`). Mounting at `/app/` means the mux's own redirect of `/app` is not counted as a hit.
- `/api/*` — JSON/API-style endpoints (`GET /api/healthz`); the bare `/api/` catch-all is a no-op `apiHandler`.
- `/admin/*` — `GET /admin/metrics` (HTML hit count) and `POST /admin/reset` (zeroes the counter).

`apiConfig` holds shared state (`fileserverHits`, an `atomic.Int32`); handlers that need it are methods on `*apiConfig`, stateless ones are plain functions.

Logging uses `log/slog` with a TextHandler whose `ReplaceAttr` forces timestamps to UTC (RFC 3339).
