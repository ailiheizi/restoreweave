#!/usr/bin/env bash
# Native Linux qualification harness for the pinned local semantic bundle and
# the in-tree raw/zstd candidate measurements. This is evidence collection,
# not a release or NAS qualification claim.
set -euo pipefail
export LC_ALL=C

usage() {
    echo "usage: $0 --artifacts DIR [--semantic-archive FILE] [--offline]" >&2
    echo "       $0 --inspect-bundle MANIFEST BUNDLE_ROOT" >&2
    echo "       --semantic-archive FILE  use an operator-supplied local bundle archive" >&2
    echo "       --offline                require --semantic-archive and never invoke the downloader" >&2
}

ARTIFACTS=""
SEMANTIC_ARCHIVE=""
OFFLINE_ONLY=0
INSPECT_MANIFEST=""
INSPECT_ROOT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --artifacts) ARTIFACTS="$2"; shift 2 ;;
        --semantic-archive) SEMANTIC_ARCHIVE="$2"; shift 2 ;;
        --offline) OFFLINE_ONLY=1; shift ;;
        --inspect-bundle) INSPECT_MANIFEST="$2"; INSPECT_ROOT="$3"; shift 3 ;;
        -h|--help) usage; exit 0 ;;
        *) usage; exit 2 ;;
    esac
done
validate_bundle_asset_path() {
    local relative="$1"
    if [[ -z "$relative" ]]; then
        return 1
    fi
    case "$relative" in
        /*|.|..|./*|../*|*/./*|*/../*|*/.|*/..|*//*|*\\*) return 1 ;;
    esac
}
validate_bundle_manifest() {
    jq -e '.schema == "restoreweave.semantic-bundle.v1"' "$MANIFEST" >/dev/null
}
asset_path() {
    local field="$1" relative
    relative="$(jq -er --arg field "$field" '.[$field].path | select(type == "string" and length > 0)' "$MANIFEST")"
    if ! validate_bundle_asset_path "$relative"; then
        echo "error: unsafe bundle asset path: $relative" >&2
        return 1
    fi
    printf '%s/%s\n' "$BUNDLE_ROOT" "$relative"
}
if [[ -n "$INSPECT_MANIFEST" || -n "$INSPECT_ROOT" ]]; then
    if [[ -z "$INSPECT_MANIFEST" || -z "$INSPECT_ROOT" || -n "$ARTIFACTS" || -n "$SEMANTIC_ARCHIVE" || "$OFFLINE_ONLY" -eq 1 ]]; then
        usage
        exit 2
    fi
    if ! command -v jq >/dev/null 2>&1; then
        echo "error: required tool is unavailable: jq" >&2
        exit 1
    fi
    MANIFEST="$INSPECT_MANIFEST"
    BUNDLE_ROOT="$INSPECT_ROOT"
    if ! validate_bundle_manifest; then
        echo "error: bundle manifest is not restoreweave.semantic-bundle.v1" >&2
        exit 1
    fi
    for field in runtime model tokenizer zvec; do
        asset_path "$field"
    done
    exit 0
fi
if [[ -z "$ARTIFACTS" ]]; then
    usage
    exit 2
fi
if [[ "$OFFLINE_ONLY" -eq 1 && -z "$SEMANTIC_ARCHIVE" ]]; then
    echo "error: --offline requires --semantic-archive FILE" >&2
    exit 2
fi
if [[ "$(uname -s)" != Linux || "$(uname -m)" != aarch64 ]]; then
    echo "error: qualification requires native Linux arm64" >&2
    exit 1
fi
for tool in bash bwrap go jq ps sha256sum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: required tool is unavailable: $tool" >&2
        exit 1
    fi
done

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
ARTIFACTS="$(mkdir -p "$ARTIFACTS" && cd "$ARTIFACTS" && pwd -P)"
if [[ -n "$SEMANTIC_ARCHIVE" ]]; then
    if [[ "$SEMANTIC_ARCHIVE" != /* || ! -f "$SEMANTIC_ARCHIVE" || -L "$SEMANTIC_ARCHIVE" ]]; then
        echo "error: --semantic-archive must be an absolute regular non-symlink file" >&2
        exit 2
    fi
    archive_parent="$(dirname "$SEMANTIC_ARCHIVE")"
    archive_base="$(basename "$SEMANTIC_ARCHIVE")"
    archive_parent="$(cd "$archive_parent" && pwd -P)" || {
        echo "error: cannot resolve semantic archive parent" >&2
        exit 2
    }
    SEMANTIC_ARCHIVE="$archive_parent/$archive_base"
fi
QUALIFICATION_ROOT="$(mktemp -d)"
DAEMON_PID=""
cleanup() {
    if [[ -n "$DAEMON_PID" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
        kill -TERM "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    rm -rf "$QUALIFICATION_ROOT"
}
trap cleanup EXIT

export XDG_CONFIG_HOME="$QUALIFICATION_ROOT/config-home"
export XDG_DATA_HOME="$QUALIFICATION_ROOT/data-home"
CONFIG_PATH="$XDG_CONFIG_HOME/restoreweave/config.toml"
SOCKET_PATH="$QUALIFICATION_ROOT/restoreweave.sock"
BIN_DIR="$QUALIFICATION_ROOT/bin"
mkdir -p "$BIN_DIR"

# Copy the operator archive into the clean qualification root.  The source is
# never modified, and the temporary copy is removed before the restart below;
# that makes the restart/readback path independent of the archive input.
OFFLINE_ARCHIVE=""
if [[ -n "$SEMANTIC_ARCHIVE" ]]; then
    OFFLINE_ARCHIVE="$QUALIFICATION_ROOT/semantic-bundle.tar.gz"
    cp "$SEMANTIC_ARCHIVE" "$OFFLINE_ARCHIVE"
    sha256sum "$SEMANTIC_ARCHIVE" | tee "$ARTIFACTS/semantic-archive.sha256"
fi

cd "$REPO_ROOT"
go version | tee "$ARTIFACTS/go-version.txt"
go test ./... -run '^$' | tee "$ARTIFACTS/compile.log"
go test ./... -count=1 | tee "$ARTIFACTS/full-test.log"
go build -tags=purego -o "$BIN_DIR/rw" ./client/cmd/rw
go build -tags=purego -o "$BIN_DIR/restoreweaved" ./server/cmd/restoreweaved
"$BIN_DIR/rw" config init --path "$CONFIG_PATH" | tee "$ARTIFACTS/config-init.log"

"$BIN_DIR/restoreweaved" --config "$CONFIG_PATH" --socket "$SOCKET_PATH" \
    >"$ARTIFACTS/installer-daemon.log" 2>&1 &
DAEMON_PID=$!
for _ in $(seq 1 120); do
    if "$BIN_DIR/rw" --socket "$SOCKET_PATH" status >/dev/null 2>&1; then
        break
    fi
    if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
        echo "error: daemon exited before semantic installation" >&2
        exit 1
    fi
    sleep 1
done
if ! "$BIN_DIR/rw" --socket "$SOCKET_PATH" status >/dev/null 2>&1; then
    echo "error: daemon did not become ready" >&2
    exit 1
fi
install_args=(semantic bundle install)
if [[ -n "$OFFLINE_ARCHIVE" ]]; then
    install_args+=(--archive "$OFFLINE_ARCHIVE")
fi
"$BIN_DIR/rw" --socket "$SOCKET_PATH" --timeout 15m --json "${install_args[@]}" \
    | tee "$ARTIFACTS/semantic-install.json"
# The installed bundle, rather than the archive, is the only input to the
# restarted daemon.  Never remove the operator's original archive.
if [[ -n "$OFFLINE_ARCHIVE" ]]; then
    rm -f "$OFFLINE_ARCHIVE"
fi
kill -TERM "$DAEMON_PID"
wait "$DAEMON_PID"
DAEMON_PID=""

BUNDLE_ROOT="$XDG_DATA_HOME/restoreweave/models/bge-small-zh-v1.5/linux-arm64"
MANIFEST="$BUNDLE_ROOT/semantic-bundle.json"
if [[ ! -f "$MANIFEST" || -L "$MANIFEST" ]]; then
    echo "error: operator installation did not publish the pinned bundle manifest" >&2
    exit 1
fi
if ! validate_bundle_manifest; then
    echo "error: installed bundle manifest is not restoreweave.semantic-bundle.v1" >&2
    exit 1
fi
cp "$MANIFEST" "$ARTIFACTS/semantic-bundle.json"
export RESTOREWEAVE_BGE_ONNX_RUNTIME="$(asset_path runtime)"
export RESTOREWEAVE_BGE_ONNX_MODEL="$(asset_path model)"
export RESTOREWEAVE_BGE_TOKENIZER="$(asset_path tokenizer)"
export RESTOREWEAVE_ZVEC_LIBRARY="$(asset_path zvec)"
export RESTOREWEAVE_SEMANTIC_BUNDLE_ROOT="$BUNDLE_ROOT"
export RESTOREWEAVE_RUN_SUPERVISED_ONNX=1
export RESTOREWEAVE_SEMANTIC_QUALIFICATION_REPORT="$ARTIFACTS/semantic-qualification.json"
for asset in "$RESTOREWEAVE_BGE_ONNX_RUNTIME" "$RESTOREWEAVE_BGE_ONNX_MODEL" \
    "$RESTOREWEAVE_BGE_TOKENIZER" "$RESTOREWEAVE_ZVEC_LIBRARY"; do
    if [[ ! -f "$asset" || -L "$asset" ]]; then
        echo "error: installed bundle asset is unavailable: $asset" >&2
        exit 1
    fi
done

run_required_test() {
    local package="$1" test_name="$2" output="$3"
    go test -json -tags='purego supervised_integration' "$package" \
        -run "^${test_name}$" -count=1 | tee "$output"
    if jq -e --arg test "$test_name" 'select(.Test == $test and .Action == "skip")' "$output" >/dev/null; then
        echo "error: required real qualification test skipped: $test_name" >&2
        exit 1
    fi
    if ! jq -e --arg test "$test_name" 'select(.Test == $test and .Action == "pass")' "$output" >/dev/null; then
        echo "error: required real qualification test did not pass: $test_name" >&2
        exit 1
    fi
}

run_required_test ./server/internal/processor TestSupervisedONNXSemanticQualification \
    "$ARTIFACTS/semantic-qualification-test.jsonl"
run_required_test ./server/internal/processor TestRealBGEEmbeddingBuildsAndQueriesNativeZvec \
    "$ARTIFACTS/zvec-component-test.jsonl"
run_required_test ./server/cmd/restoreweaved TestRealDaemonSemanticEndToEnd \
    "$ARTIFACTS/semantic-daemon-test.jsonl"

"$REPO_ROOT/scripts/savings-report.sh" --corpus-profile heterogeneous \
    --work-dir "$ARTIFACTS/repository-candidate" --profile both --keep \
    | tee "$ARTIFACTS/repository-candidate.log"

jq -n \
    --arg schema restoreweave.native-linux-qualification.v1 \
    --arg os "$(uname -s)" --arg arch "$(uname -m)" \
    --arg kernel "$(uname -r)" --arg go "$(go version)" \
    --arg install_mode "$([[ -n "$SEMANTIC_ARCHIVE" ]] && printf offline-archive || printf development-downloader)" \
    '{schema:$schema, scope:"native Linux arm64 CI; not NAS or release qualification", os:$os, arch:$arch, kernel:$kernel, go:$go, semantic_install_mode:$install_mode}' \
    >"$ARTIFACTS/host.json"
