#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "$*" >&2
  exit 1
}

copy_check_fixture() {
  local tmp="$1"
  mkdir -p "$tmp/scripts" "$tmp/server"
  cp "$ROOT_DIR/scripts/check.sh" "$tmp/scripts/check.sh"
  cat >"$tmp/scripts/local-env.sh" <<'STUB'
PORT="${PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
STUB
  cat >"$tmp/.env" <<'STUB'
POSTGRES_DB=multica_test
POSTGRES_USER=multica
POSTGRES_PASSWORD=multica
STUB
}

test_setup_failure_does_not_report_success() {
  local tmp out status
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  copy_check_fixture "$tmp"
  cat >"$tmp/scripts/ensure-postgres.sh" <<'STUB'
#!/usr/bin/env bash
echo "forced setup failure" >&2
exit 42
STUB
  chmod +x "$tmp/scripts/ensure-postgres.sh"
  mkdir -p "$tmp/stub-bin"
  cat >"$tmp/stub-bin/docker" <<'STUB'
#!/usr/bin/env bash
echo "forced docker setup failure" >&2
exit 42
STUB
  chmod +x "$tmp/stub-bin/docker"

  set +e
  (cd "$tmp" && PATH="$tmp/stub-bin:/usr/bin:/bin" bash scripts/check.sh >"$tmp/out" 2>&1)
  status=$?
  set -e
  out="$(cat "$tmp/out")"

  [ "$status" -ne 0 ] || fail "check.sh exited 0 after setup failure"
  ! grep -Fq "All checks passed" <<<"$out" || fail "check.sh printed success after setup failure"
  grep -Fq "Checks FAILED" <<<"$out" || fail "check.sh did not print failure banner"
}

test_local_check_db_uses_throwaway_docker_run_and_cleans_up() {
  local tmp stub_bin log out status
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  copy_check_fixture "$tmp"
  stub_bin="$tmp/stub-bin"
  log="$tmp/docker.log"
  mkdir -p "$stub_bin"

  cat >"$stub_bin/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_STUB_LOG"
case "$1" in
  run)
    [[ "$*" == *" --name multica-check-"* ]] || exit 11
    [[ "$*" == *" -p 127.0.0.1:55"*":5432 "* ]] || exit 12
    echo test-container-id
    ;;
  exec)
    exit 0
    ;;
  rm)
    [[ "$*" == *" -f multica-check-"* ]] || exit 13
    ;;
  *)
    echo "unexpected docker command: $*" >&2
    exit 10
    ;;
esac
STUB
  chmod +x "$stub_bin/docker"

  cat >"$stub_bin/pnpm" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  typecheck|test)
    exit 0
    ;;
  exec)
    exit 0
    ;;
esac
echo "unexpected pnpm command: $*" >&2
exit 20
STUB
  chmod +x "$stub_bin/pnpm"

  cat >"$stub_bin/go" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  chmod +x "$stub_bin/go"

  cat >"$stub_bin/curl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  chmod +x "$stub_bin/curl"

  set +e
  (cd "$tmp" && PATH="$stub_bin:/usr/bin:/bin" DOCKER_STUB_LOG="$log" bash scripts/check.sh >"$tmp/out" 2>&1)
  status=$?
  set -e
  out="$(cat "$tmp/out")"

  [ "$status" -eq 0 ] || fail "check.sh exited non-zero: $out"
  grep -Fq "run" "$log" || fail "docker run was not called"
  grep -Fq "rm -f multica-check-" "$log" || fail "throwaway DB container was not cleaned up"
  ! grep -Fq "docker compose" "$log" || fail "check.sh used docker compose"
}

test_setup_failure_does_not_report_success
test_local_check_db_uses_throwaway_docker_run_and_cleans_up
echo "check.sh tests passed"
