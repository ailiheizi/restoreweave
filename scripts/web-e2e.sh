#!/bin/sh
set -eu

# Opt-in browser harness. It owns only a temporary daemon process and a
# temporary config/data root; it never changes the repository or a user's
# persisted RestoreWeave profile.
if [ "${RESTOREWEAVE_WEB_E2E:-0}" != "1" ]; then
  echo "SKIP: set RESTOREWEAVE_WEB_E2E=1 to run the browser harness"
  exit 0
fi

daemon_bin=${RESTOREWEAVE_DAEMON_BIN:-restoreweaved}
rw_bin=${RESTOREWEAVE_RW_BIN:-rw}
api_listen=${RESTOREWEAVE_WEB_E2E_API_LISTEN:-127.0.0.1:4536}

command -v "$daemon_bin" >/dev/null 2>&1 || { echo "SKIP: RESTOREWEAVE_DAEMON_BIN is not available"; exit 0; }
command -v "$rw_bin" >/dev/null 2>&1 || { echo "SKIP: RESTOREWEAVE_RW_BIN is not available"; exit 0; }
command -v curl >/dev/null 2>&1 || { echo "SKIP: curl is not available"; exit 0; }
command -v node >/dev/null 2>&1 || { echo "SKIP: node is not available"; exit 0; }
command -v npm >/dev/null 2>&1 || { echo "SKIP: npm is not available"; exit 0; }
command -v cmp >/dev/null 2>&1 || { echo "SKIP: cmp is not available"; exit 0; }

playwright_module=${RESTOREWEAVE_PLAYWRIGHT_MODULE:-}
# When Playwright is available but its managed browser cache is not, set
# RESTOREWEAVE_PLAYWRIGHT_EXECUTABLE_PATH to an installed Chrome/Chromium.
# The variable is inherited by each phase invocation below.
if [ -z "$playwright_module" ] && [ -f "web/node_modules/playwright/index.mjs" ]; then
  playwright_module=$(pwd)/web/node_modules/playwright/index.mjs
fi
if [ -z "$playwright_module" ] || ! node --input-type=module -e 'import(process.argv[1]).catch(() => process.exit(77))' "$playwright_module" >/dev/null 2>&1; then
  echo "SKIP: optional npm package playwright is not installed"
  echo "      install playwright under web/node_modules or set RESTOREWEAVE_PLAYWRIGHT_MODULE"
  exit 0
fi

npm --prefix web run build >/dev/null || { echo "FAIL: WebUI production build failed" >&2; exit 1; }

tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/restoreweave-web-e2e.XXXXXX")
daemon_pid=
cleanup() {
  if [ -n "$daemon_pid" ]; then kill "$daemon_pid" 2>/dev/null || true; fi
  rm -rf "$tmp_root"
}
trap cleanup EXIT INT TERM

config_path=$tmp_root/config.toml
source_path=$tmp_root/source
socket_path=$tmp_root/restoreweaved.sock
web_root=$(cd web/dist 2>/dev/null && pwd -P) || { echo "SKIP: web/dist is not built; run npm --prefix web run build"; exit 0; }
[ -f "$web_root/index.html" ] || { echo "SKIP: web/dist/index.html is missing; run npm --prefix web run build"; exit 0; }
mkdir -p "$source_path"
printf '%s\n' 'RestoreWeave browser E2E durable duplicate content' > "$source_path/alpha.txt"
cp "$source_path/alpha.txt" "$source_path/beta.txt"
restore_destination=$tmp_root/restored

"$rw_bin" config init --path "$config_path" >/dev/null
RESTOREWEAVE_CATALOG="$tmp_root/catalog.sqlite" \
RESTOREWEAVE_REPOSITORY="$tmp_root/repository" \
RESTOREWEAVE_VECTORS="$tmp_root/vectors" \
RESTOREWEAVE_MODELS="$tmp_root/models" \
RESTOREWEAVE_RECOVERY_RECORDS="$tmp_root/recovery" \
"$daemon_bin" --config "$config_path" --socket "$socket_path" --api-listen "$api_listen" --web-root "$web_root" >"$tmp_root/daemon.log" 2>&1 &
daemon_pid=$!

api_base=http://$api_listen
i=0
while ! curl -fsS "$api_base/api/v1/healthz" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "daemon did not become ready" >&2; cat "$tmp_root/daemon.log" >&2; exit 1; }
  sleep 0.1
done

web_url=$api_base/
i=0
while ! curl -fsS "$web_url" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "daemon WebUI did not become ready" >&2; cat "$tmp_root/daemon.log" >&2; exit 1; }
  sleep 0.1
done

RESTOREWEAVE_WEB_E2E_URL="$web_url" \
RESTOREWEAVE_WEB_E2E_SOURCE="$source_path" \
RESTOREWEAVE_WEB_E2E_RESTORE_DEST="$restore_destination" \
RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" \
node scripts/web-e2e.mjs online

cmp -s "$source_path/alpha.txt" "$restore_destination/alpha.txt" || { echo "restored alpha bytes differ" >&2; exit 1; }
cmp -s "$source_path/beta.txt" "$restore_destination/beta.txt" || { echo "restored beta bytes differ" >&2; exit 1; }
sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}
[ "$(sha256_file "$source_path/alpha.txt")" = "$(sha256_file "$restore_destination/alpha.txt")" ] || { echo "alpha SHA-256 differs" >&2; exit 1; }
[ "$(sha256_file "$source_path/beta.txt")" = "$(sha256_file "$restore_destination/beta.txt")" ] || { echo "beta SHA-256 differs" >&2; exit 1; }

RESTOREWEAVE_WEB_E2E_URL="$web_url" RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" node scripts/web-e2e.mjs offline

kill "$daemon_pid"
wait "$daemon_pid" 2>/dev/null || true
daemon_pid=

RESTOREWEAVE_CATALOG="$tmp_root/catalog.sqlite" \
RESTOREWEAVE_REPOSITORY="$tmp_root/repository" \
RESTOREWEAVE_VECTORS="$tmp_root/vectors" \
RESTOREWEAVE_MODELS="$tmp_root/models" \
RESTOREWEAVE_RECOVERY_RECORDS="$tmp_root/recovery" \
"$daemon_bin" --config "$config_path" --socket "$socket_path" --api-listen "$api_listen" --web-root "$web_root" >"$tmp_root/daemon-restarted.log" 2>&1 &
daemon_pid=$!
i=0
while ! curl -fsS "$api_base/api/v1/healthz" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 80 ] || { echo "daemon did not recover" >&2; cat "$tmp_root/daemon-restarted.log" >&2; exit 1; }
  sleep 0.1
done
RESTOREWEAVE_WEB_E2E_URL="$web_url" RESTOREWEAVE_PLAYWRIGHT_MODULE="$playwright_module" node scripts/web-e2e.mjs recovered

echo "PASS: RestoreWeave daemon/WebUI browser E2E"
