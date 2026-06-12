#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example, or run 'make worktree-env' and use .env.worktree."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-multica}"
POSTGRES_USER="${POSTGRES_USER:-multica}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-multica}"
DATABASE_URL="${DATABASE_URL:-}"

export PGPASSWORD="$POSTGRES_PASSWORD"

db_host=""
db_port="${MULTICA_DEV_PG_PORT:-${POSTGRES_PORT:-5432}}"
db_name="$POSTGRES_DB"

parse_database_url() {
  local rest authority hostport path port_part

  rest="${DATABASE_URL#*://}"
  rest="${rest%%\?*}"
  authority="${rest%%/*}"
  path="${rest#*/}"

  if [ "$authority" = "$rest" ]; then
    path=""
  fi

  hostport="${authority##*@}"

  if [[ "$hostport" == \[* ]]; then
    db_host="${hostport#\[}"
    db_host="${db_host%%]*}"
    port_part="${hostport#*\]}"
    if [[ "$port_part" == :* ]] && [ -n "${port_part#:}" ]; then
      db_port="${port_part#:}"
    fi
  else
    db_host="${hostport%%:*}"
    if [[ "$hostport" == *:* ]] && [ -n "${hostport##*:}" ]; then
      db_port="${hostport##*:}"
    fi
  fi

  if [ -n "$path" ]; then
    db_name="${path%%/*}"
  fi
}

if [ -n "$DATABASE_URL" ]; then
  parse_database_url
fi

MULTICA_DEV_PG_PORT="${MULTICA_DEV_PG_PORT:-$db_port}"
POSTGRES_PORT="${POSTGRES_PORT:-$db_port}"
MULTICA_DEV_PROJECT="${MULTICA_DEV_PROJECT:-multica-dev}"
export MULTICA_DEV_PROJECT MULTICA_DEV_PG_PORT POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD

is_local() {
  [ -z "$DATABASE_URL" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

owned_postgres_container() {
  [ -n "$(docker compose ps -q postgres 2>/dev/null || true)" ]
}

port_is_busy() {
  local port="$1"

  if command -v ss > /dev/null 2>&1; then
    ss -H -ltn "sport = :$port" 2>/dev/null | grep -q . && return 0
  fi
  if command -v lsof > /dev/null 2>&1; then
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1 && return 0
  fi
  if command -v nc > /dev/null 2>&1; then
    nc -z 127.0.0.1 "$port" >/dev/null 2>&1 && return 0
  fi
  return 1
}

print_busy_port_help() {
  local task_name="${MULTICA_DEV_PROJECT:-multica-dev}-pg"
  cat <<EOF
ERROR: Configured PostgreSQL port $db_port is already in use, and no postgres container belongs to compose project '$MULTICA_DEV_PROJECT'.

Use a free per-task 55xxx port and point DATABASE_URL at it instead, for example:

  docker run -d --name ${task_name} -p 127.0.0.1:55xxx:5432 -e POSTGRES_PASSWORD=test pgvector/pgvector:pg17
  export DATABASE_URL=postgres://postgres:test@127.0.0.1:55xxx/postgres?sslmode=disable

Then re-run the command. Do not bind 5432 and do not reuse a compose project you did not create.
EOF
}

if is_local; then
  # ---------- Local: use this checkout's configured Docker Compose project ----------
  echo "==> Ensuring PostgreSQL for compose project '$MULTICA_DEV_PROJECT' on 127.0.0.1:$db_port..."

  if ! owned_postgres_container && port_is_busy "$db_port"; then
    print_busy_port_help
    exit 1
  fi

  docker compose up -d postgres

  echo "==> Waiting for PostgreSQL to be ready..."
  until docker compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres > /dev/null 2>&1; do
    sleep 1
  done

  echo "==> Ensuring database '$POSTGRES_DB' exists..."
  db_exists="$(docker compose exec -T postgres \
    psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

  if [ "$db_exists" != "1" ]; then
    docker compose exec -T postgres \
      psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE \"$POSTGRES_DB\"" \
      > /dev/null
  fi

  echo "✓ PostgreSQL ready (local Docker). Project: $MULTICA_DEV_PROJECT. Database: $POSTGRES_DB"
else
  # ---------- Remote: skip Docker, verify connectivity ----------
  echo "==> Remote database detected (host: $db_host). Skipping Docker."
  if command -v pg_isready > /dev/null 2>&1; then
    echo "==> Waiting for PostgreSQL at $db_host:$db_port to be ready..."
    until pg_isready -d "$DATABASE_URL" > /dev/null 2>&1; do
      sleep 1
    done
    echo "✓ PostgreSQL ready (remote: $db_host:$db_port). Database: $db_name"
  else
    echo "==> pg_isready not found. Skipping remote connectivity preflight."
    echo "✓ PostgreSQL configured (remote: $db_host:$db_port). Database: $db_name"
  fi
fi
