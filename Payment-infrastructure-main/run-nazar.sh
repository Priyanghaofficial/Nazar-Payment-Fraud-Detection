#!/usr/bin/env bash
# Boots Nazar's backend from cold: containers -> migrations -> secrets -> engine.
# Safe to re-run; every step is idempotent.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$PWD"
ENGINE="${CONTAINER_ENGINE:-podman}"
PG=nazar-postgres-5433   # 5432 is left free for the other project's Docker stack
PGPORT=5433

say() { printf '\033[36m==>\033[0m %s\n' "$*"; }

say "starting Redis + Postgres ($ENGINE)"
$ENGINE start nazar-redis "$PG" >/dev/null 2>&1 || {
  $ENGINE run -d --name nazar-redis -p 6379:6379 \
    docker.io/redis:7-alpine redis-server --appendonly no >/dev/null 2>&1 || true
  $ENGINE run -d --name "$PG" -p $PGPORT:5432 \
    -e POSTGRES_USER=nazar -e POSTGRES_PASSWORD=nazar -e POSTGRES_DB=nazar \
    docker.io/postgres:16-alpine >/dev/null 2>&1 || true
}

say "waiting for Postgres"
for _ in $(seq 1 30); do
  $ENGINE exec "$PG" pg_isready -U nazar >/dev/null 2>&1 && break
  sleep 1
done

say "applying migrations"
for m in sql/migrations/*.sql; do
  $ENGINE exec -i "$PG" psql -qU nazar -d nazar <"$m" >/dev/null 2>&1 || true
done

# Groq credentials for the LLM explanation lane. Absent -> the deterministic
# narrator takes over and nothing else changes.
if [ -f .env.local ]; then
  say "loading .env.local"
  set -a; . ./.env.local; set +a
fi

export NAZAR_REPO_ROOT="$ROOT"
export NAZAR_REDIS_CONTAINER=nazar-redis
export POSTGRES_DSN="postgres://nazar:nazar@localhost:$PGPORT/nazar?sslmode=disable"
export REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"

say "starting engine on :8080"
cd go && exec go run ./cmd/nazar
