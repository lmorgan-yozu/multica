#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "$*" >&2
  exit 1
}

test_worktree_env_generates_unique_compose_project_and_pg_port() {
  local tmp env_file out project port
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  env_file="$tmp/.env.worktree"

  (cd "$tmp" && bash "$ROOT_DIR/scripts/init-worktree-env.sh" "$env_file" >"$tmp/out")
  out="$(cat "$tmp/out")"

  project="$(grep '^MULTICA_DEV_PROJECT=' "$env_file" | cut -d= -f2)"
  port="$(grep '^POSTGRES_PORT=' "$env_file" | cut -d= -f2)"

  [[ "$project" == multica-dev-* ]] || fail "unexpected compose project: $project"
  [ "$project" != "multica" ] || fail "compose project must not be multica"
  [ "$project" != "multica-prod" ] || fail "compose project must not be multica-prod"
  [[ "$port" == 55??? ]] || fail "postgres port must be in 55xxx range: $port"
  grep -Fq "MULTICA_DEV_PG_PORT=$port" "$env_file" || fail "MULTICA_DEV_PG_PORT does not mirror POSTGRES_PORT"
  grep -Fq "127.0.0.1:${port}" "$env_file" || fail "DATABASE_URL does not use generated port"
  grep -Fq "Shared Postgres: 127.0.0.1:${port}" <<<"$out" || fail "summary does not show generated port"
}

test_compose_config_uses_configured_project_and_pg_port() {
  local out

  out="$(
    cd "$ROOT_DIR" && \
      MULTICA_DEV_PROJECT=multica-dev-testcase \
      MULTICA_DEV_PG_PORT=55991 \
      POSTGRES_PORT=55991 \
      docker compose config
  )"

  grep -Fq 'name: multica-dev-testcase' <<<"$out" || fail "compose config did not use configured project"
  grep -Fq 'host_ip: 127.0.0.1' <<<"$out" || fail "compose config did not bind loopback"
  grep -Fq 'published: "55991"' <<<"$out" || fail "compose config did not use configured pg port"
  ! grep -Fq 'name: multica-prod' <<<"$out" || fail "compose config resolved to prod project"
  ! grep -Fq '127.0.0.1:5432:5432' <<<"$out" || fail "compose config used default 5432 bind"
}

test_ensure_postgres_refuses_unowned_busy_port() {
  local tmp stub_bin out status
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  stub_bin="$tmp/stub-bin"
  mkdir -p "$stub_bin"

  cp "$ROOT_DIR/scripts/ensure-postgres.sh" "$tmp/ensure-postgres.sh"
  cat >"$tmp/.env.worktree" <<'STUB'
MULTICA_DEV_PROJECT=multica-dev-busy
MULTICA_DEV_PG_PORT=55992
POSTGRES_DB=multica_busy
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
POSTGRES_PORT=55992
DATABASE_URL=postgres://multica:multica@127.0.0.1:55992/multica_busy?sslmode=disable
STUB

  cat >"$stub_bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_STUB_LOG"
case "$*" in
  "compose ps -q postgres")
    exit 0
    ;;
  *)
    echo "unexpected docker command: $*" >&2
    exit 20
    ;;
esac
STUB
  chmod +x "$stub_bin/docker"

  cat >"$stub_bin/pg_isready" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
  chmod +x "$stub_bin/pg_isready"

  cat >"$stub_bin/ss" <<'STUB'
#!/usr/bin/env bash
echo 'LISTEN 0 4096 127.0.0.1:55992 0.0.0.0:*'
exit 0
STUB
  chmod +x "$stub_bin/ss"

  set +e
  (
    cd "$tmp" && \
      PATH="$stub_bin:/usr/bin:/bin" \
      DOCKER_STUB_LOG="$tmp/docker.log" \
      bash ./ensure-postgres.sh .env.worktree >"$tmp/out" 2>&1
  )
  status=$?
  set -e
  out="$(cat "$tmp/out")"

  [ "$status" -ne 0 ] || fail "ensure-postgres succeeded with busy unowned port"
  grep -Fq "Configured PostgreSQL port 55992 is already in use" <<<"$out" || fail "missing busy-port error: $out"
  grep -Fq "docker run -d --name" <<<"$out" || fail "missing throwaway docker instructions"
  ! grep -Fq "compose up" "$tmp/docker.log" || fail "ensure-postgres tried docker compose up on busy port"
}

test_worktree_env_generates_unique_compose_project_and_pg_port
test_compose_config_uses_configured_project_and_pg_port
test_ensure_postgres_refuses_unowned_busy_port
echo "dev postgres tests passed"
