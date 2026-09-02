#!/bin/sh
set -eu

# Opt-in browser harness. It owns only temporary daemon/Vite processes and a
# temporary config/data root; it never changes the repository or a user's
# persisted RestoreWeave profile.
if [ "${RESTOREWEAVE_WEB_E2E:-0}" != "1" ]; then
  echo "SKIP: set RESTOREWEAVE_WEB_E2E=1 to run the browser harness"
  exit 0
fi

daemon_bin=${RESTOREWEAVE_DAEMON_BIN:-restoreweaved}
rw_bin=${RESTOREWEAVE_RW_BIN:-rw}
api_listen=${RESTOREWEAVE_WEB_E2E_API_LISTEN:-127.0.0.1:4536}
web_port=${RESTOREWEAVE_WEB_E2E_WEB_PORT:-5174}

command -v "$daemon_bin" >/dev/null 2>&1 || { echo "SKIP: RESTOREWEAVE_DAEMON_BIN is not available"; exit 0; }
command -v "$rw_bin" >/dev/null 2>&1 || { echo "SKIP: RESTOREWEAVE_RW_BIN is not available"; exit 0; }
command -v curl >/dev/null 2>&1 || { echo "SKIP: curl is not available"; exit 0; }
command -v node >/dev/null 2>&1 || { echo "SKIP: node is not available"; exit 0; }

playwright_module=${RESTOREWEAVE_PLAYWRIGHT_MODULE:-}
if [ -z "$playwright_module" ] && [ -f "web/node_modules/playwright/index.mjs" ]; then
  playwright_module=$(pwd)/web/node_modules/playwright/index.mjs
fi
if [ -z "$playwright_module" ] || ! node --input-type=module -e 'import(process.argv[1]).catch(() => process.exit(77))' "$playwright_module" >/dev/null 2>&1; then
  echo "SKIP: optional npm package playwright is not installed"
  echo "      install playwright under web/node_modules or set RESTOREWEAVE_PLAYWRIGHT_MODULE"
  exit 0
fi

tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/restoreweave-web-e2e.XXXXXX")
daemon_pid=
vite_pid=
cleanup() {
  if [ -n "$vite_pid" ]; then kill "$vite_pid" 2>/dev/null || true; fi
  if [ -n "$daemon_pid" ]; then kill "$daemon_pid" 2>/dev/null || true; fi
  rm -rf "$tmp_root"
}
trap cleanup EXIT INT TERM

config_path=$tmp_root/config.toml
source_path=$tmp_root/source
socket_path=$tmp_root/restoreweaved.sock
mkdir -p "$source_path"
printf '%s\n' 'RestoreWeave browser E2E durable content' > "$source_path/browser-e2e.txt"

"$rw_bin" config init --path "$config_path" >/dev/null
RESTOREWEAVE_CATALOG="$tmp_root/catalog.sqlite" \
RESTOREWEAVE_REPOSITORY="$tmp_root/repository" \
RESTOREWEAVE_VECTORS="$tmp_root/vectors" \
RESTOREWEAVE_MODELS="$tmp_root/models" \
RESTOREWEAVE_RECOVERY_RECORDS="$tmp_root/recovery" \
"$daemon_bin" --config "$config_path" --socket "$socket_path" --api-listen "$api_listen" >"$tmp_root/daemon.log" 2>&1 &
daemon_pid=$!

api_base=http://$api_listen
i=0
while ! curl -fsS "$api_base/healthz" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "daemon did not become ready" >&2; cat "$tmp_root/daemon.log" >&2; exit 1; }
  sleep 0.1
done

RESTOREWEAVE_API_TARGET="$api_base" npm --prefix web run dev -- --host 127.0.0.1 --port "$web_port" >"$tmp_root/vite.log" 2>&1 &
vite_pid=$!
web_url=http://127.0.0.1:$web_port/
i=0
while ! curl -fsS "$web_url" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "Vite did not become ready" >&2; cat "$tmp_root/vite.log" >&2; exit 1; }
  sleep 0.1
done

RESTOREWEAVE_WEB_E2E_URL="$web_url" \
RESTOREWEAVE_WEB_E2E_SOURCE="$source_path" \
RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" \
node scripts/web-e2e.mjs online

kill "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=
RESTOREWEAVE_WEB_E2E_URL="$web_url" RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" node scripts/web-e2e.mjs offline

RESTOREWEAVE_CATALOG="$tmp_root/catalog.sqlite" \
RESTOREWEAVE_REPOSITORY="$tmp_root/repository" \
RESTOREWEAVE_VECTORS="$tmp_root/vectors" \
RESTOREWEAVE_MODELS="$tmp_root/models" \
RESTOREWEAVE_RECOVERY_RECORDS="$tmp_root/recovery" \
"$daemon_bin" --config "$config_path" --socket "$socket_path" --api-listen "$api_listen" >"$tmp_root/daemon-restarted.log" 2>&1 &
daemon_pid=$!
i=0
while ! curl -fsS "$api_base/healthz" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "daemon did not recover" >&2; cat "$tmp_root/daemon-restarted.log" >&2; exit 1; }
  sleep 0.1
done
RESTOREWEAVE_WEB_E2E_URL="$web_url" RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" node scripts/web-e2e.mjs recovered

echo "PASS: RestoreWeave daemon/WebUI browser E2E"
