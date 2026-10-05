#!/usr/bin/env bash
# Run goose migrations against the Postgres database in DB_URL.
# Usage: scripts/migrate.sh up|down|status
#   DB_URL may be exported or set in .env at the repo root,
#   e.g. DB_URL=postgres://user:pass@localhost:5432/chirpy?sslmode=disable
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
    set -a
    # shellcheck disable=SC1091
    source .env
    set +a
fi

: "${DB_URL:?DB_URL is not set (export it or add it to .env)}"

cmd="${1:-}"
case "$cmd" in
    up|down|status) ;;
    *) echo "usage: $0 up|down|status" >&2; exit 2 ;;
esac

exec goose -dir sql/schema postgres "$DB_URL" "$cmd"
