# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Chirpy is a small Go web server backed by Postgres, built as a Boot.dev backend learning project. Handlers and routing live in `main.go` (tests in `main_test.go`); password hashing is in `internal/auth`; DB access is sqlc-generated code in `internal/database`. `go-notes.md` holds the author's personal Go learning notes (not project docs).

## Commands

- Run: `go run .` (serves on `:8080`; must be run from the repo root since the static path is `./static`)
- Build: `go build -o chirpy .` (the `/chirpy` binary is gitignored)
- Test all: `go test ./...`
- Single test: `go test -run TestHandlerResetForbiddenOutsideDev ./...`
- Migrations: `scripts/migrate.sh up|down|status` (goose; reads `DB_URL` from env or `.env`)
- Regenerate DB code: `sqlc generate` (run after changing `sql/schema` or `sql/queries`)

Config comes from env / `.env` (see `.env.example`): `DB_URL` (Postgres connection string) and `PLATFORM` (`dev` enables `/admin/reset`), `JWT_SECRET` (signing key for JWTs) and `POLKA_KEY` (API key Polka sends to the webhook); the server refuses to start without either.

## Architecture

Routing is a single `http.ServeMux` built in `main()` using Go 1.22+ method-qualified patterns (e.g. `"GET /api/healthz"`):

- `/app/` — `http.FileServer` over `./static`, wrapped in `cfg.middlewareMetricsInc`. Files live under `static/app/` (so `static/app/index.html` is served at `/app/`). Mounting at `/app/` means the mux's own redirect of `/app` is not counted as a hit.
- `/api/*` — JSON endpoints: `GET /api/healthz`, `POST /api/users`, `PUT /api/users` (Bearer access token; updates the caller's own email and password, 401 if the token is missing or invalid), `POST /api/login`, `POST /api/refresh` (Bearer refresh token in, new 1-hour access token out; 401 if missing, unknown, expired or revoked), `POST /api/revoke` (Bearer refresh token in, sets `revoked_at`/`updated_at`, 204; 401 if the header is missing), `POST /api/polka/webhooks` (Polka webhook: `user.upgraded` sets `users.is_chirpy_red` and returns 204, any other event returns 204 untouched, unknown user 404; requires `Authorization: ApiKey <POLKA_KEY>`, else 401), `POST /api/chirps` (requires a Bearer JWT: 401 if invalid; the optional body `user_id` must match the token's user, else 403), `GET /api/chirps` (optional `?author_id=<uuid>` filters to one author's chirps; 400 if it isn't a UUID; optional `?sort=asc|desc` orders by `created_at`, default `asc`, 400 for any other value), `GET /api/chirps/{chirpID}`, `DELETE /api/chirps/{chirpID}` (Bearer access token: 401 if invalid, 404 if the chirp doesn't exist, 403 if the caller isn't its author, else 204); the bare `/api/` catch-all is a no-op `apiHandler`.
- `/admin/*` — `GET /admin/metrics` (HTML hit count) and `POST /admin/reset` (zeroes the counter and deletes all users; returns 403 unless `PLATFORM=dev`).

`apiConfig` holds shared state (`fileserverHits`, an `atomic.Int32`; `db`, the sqlc `*database.Queries`; `platform`; `jwtSecret`; `polkaKey`); handlers that need it are methods on `*apiConfig`, stateless ones are plain functions.

Logging uses `log/slog` with a TextHandler whose `ReplaceAttr` forces timestamps to UTC (RFC 3339).

## Database

Schema lives in `sql/schema/` as goose migrations (timestamp-prefixed; new ones must sort after existing ones), queries in `sql/queries/`, and `sqlc.yaml` generates `internal/database/` (do not edit by hand). `users.hashed_password` is `NOT NULL DEFAULT 'unset'`; `POST /api/users` stores an argon2id hash there (the password is never returned). `users.is_chirpy_red` is `BOOLEAN NOT NULL DEFAULT false`. Every endpoint that returns a user goes through `userFromDB`, which includes `is_chirpy_red` and omits the password hash. `chirps.user_id` cascades on user delete.

## Auth

`internal/auth` exposes `HashPassword` and `CheckPasswordHash` (argon2id via `github.com/alexedwards/argon2id`) and `MakeJWT` (HS256 via `github.com/golang-jwt/jwt/v5`, issuer `chirpy-access`, subject = user ID) and `ValidateJWT` (checks signature, expiry and issuer; returns the user ID). `GetBearerToken` extracts the token from the `Authorization: Bearer` header and `GetAPIKey` the key from `Authorization: ApiKey` (used by `POST /api/polka/webhooks`). `POST /api/chirps`, `DELETE /api/chirps/{chirpID}` and `PUT /api/users` use `GetBearerToken` and `ValidateJWT`. `MakeRefreshToken` returns a random 256-bit hex string, used by `POST /api/login`. `POST /api/users` uses `HashPassword`; `POST /api/login` uses `CheckPasswordHash` and `MakeJWT`, returning the user plus a `token` (1-hour access JWT) and a `refresh_token` (60 days, stored in `refresh_tokens` via `CreateRefreshToken`).
