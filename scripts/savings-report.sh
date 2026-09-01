#!/usr/bin/env bash
#=============================================================================
# savings-report.sh
#
# Measures and prints the mechanism-separated storage-savings report for the
# in-tree repository profiles (raw development CAS and the local-zstd-v1
# candidate) over a small deterministic corpus. This is candidate measurement,
# not qualification: it reports logical bytes, whole-file duplicate savings,
# compression savings, physical stored bytes, repository growth, typed overhead
# categories, and net physical savings separately and never adds the layers
# together. UNMEASURED index/model/temp categories remain visible as status.
#
# The runner is a build-tagged Go program under
# server/internal/repository/savingsreport/ and is invoked via `go run` so the
# script needs no committed binary and no daemon. It never creates a new
# product surface and no engine name becomes normative.
#
# Usage:
#   scripts/savings-report.sh [--corpus-dir DIR] [--work-dir DIR] [--profile raw|zstd|both]
#
#   --corpus-dir DIR  an existing, non-empty corpus to read (never modified)
#                     (default: a deterministic probe in a temp dir)
#   --work-dir DIR    where fresh repositories are created (default: a temp
#                     dir that is removed on exit; use --keep to retain it)
#   --profile P       raw, zstd, or both (default both)
#   --keep            keep the corpus and work dirs on exit
#
# Dependencies: bash, awk, cmp, cp, dd, find, mkdir, rm, go.
#=============================================================================
set -euo pipefail
# Keep the generated probe's text and byte-oriented awk output stable across
# hosts with different user locale settings.
export LC_ALL=C

# ---- options ---------------------------------------------------------------
KEEP=0
CORPUS_DIR=""
WORK_DIR=""
PROFILE=both
GOTMP=""
GOWORK=""

usage() {
    echo "usage: $0 [--corpus-dir DIR] [--work-dir DIR] [--profile raw|zstd|both] [--keep]" >&2
    exit 2
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --corpus-dir) CORPUS_DIR="$2"; shift 2 ;;
        --work-dir)   WORK_DIR="$2";   shift 2 ;;
        --profile)    PROFILE="$2";    shift 2 ;;
        --keep)       KEEP=1;          shift ;;
        -h|--help)    usage ;;
        *) echo "unknown option: $1" >&2; usage ;;
    esac
done

case "$PROFILE" in
    raw|zstd|both) ;;
    *) echo "error: --profile must be raw, zstd, or both" >&2; usage ;;
esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNNER="$REPO_ROOT/server/internal/repository/savingsreport"

# ---- temp setup ------------------------------------------------------------
TMP_BASE="$(mktemp -d 2>/dev/null || mktemp -d -t savings-report)"
cleanup() {
    if [[ "$KEEP" -ne 1 ]]; then rm -rf "$TMP_BASE"; fi
}
trap cleanup EXIT

GENERATED_CORPUS=0
if [[ -z "$CORPUS_DIR" ]]; then
    CORPUS_DIR="$TMP_BASE/corpus"
    GENERATED_CORPUS=1
else
    if [[ ! -d "$CORPUS_DIR" || -L "$CORPUS_DIR" ]]; then
        echo "error: --corpus-dir must be an existing real directory" >&2
        exit 1
    fi
fi
if [[ -z "$WORK_DIR" ]]; then WORK_DIR="$TMP_BASE/work"; fi
if [[ -e "$WORK_DIR" || -L "$WORK_DIR" ]]; then
    if [[ -L "$WORK_DIR" || ! -d "$WORK_DIR" ]]; then
        echo "error: --work-dir must be a real directory or a new path" >&2
        exit 1
    fi
    if [[ -n "$(find "$WORK_DIR" -mindepth 1 -print -quit 2>/dev/null)" ]]; then
        echo "error: --work-dir must be empty" >&2
        exit 1
    fi
fi
if [[ ! -d "$(dirname "$WORK_DIR")" ]]; then
    echo "error: parent directory for --work-dir must already exist" >&2
    exit 1
fi
if [[ "$GENERATED_CORPUS" -eq 1 ]]; then
    mkdir -p "$CORPUS_DIR"
fi
CORPUS_ABS="$(cd "$CORPUS_DIR" && pwd -P)"
if [[ -e "$WORK_DIR" ]]; then
    WORK_ABS="$(cd "$WORK_DIR" && pwd -P)"
else
    WORK_PARENT="$(cd "$(dirname "$WORK_DIR")" && pwd -P)"
    WORK_ABS="$WORK_PARENT/$(basename "$WORK_DIR")"
fi
case "$WORK_ABS" in
    "$CORPUS_ABS"|"$CORPUS_ABS"/*) echo "error: work and corpus directories must be separate and non-nested" >&2; exit 1 ;;
esac
case "$CORPUS_ABS" in
    "$WORK_ABS"|"$WORK_ABS"/*) echo "error: work and corpus directories must be separate and non-nested" >&2; exit 1 ;;
esac

# ---- deterministic corpus --------------------------------------------------
# Small fixed corpus with the four behaviors the report must separate:
#   texts/  near-identical text files (dedup-friendly)
#   dupe_0/dupe_1/  identical files under two directories (dedup target)
#   zeros/  all-zero files (compress target)
#   rand/   seeded-pseudo-random binaries (incompressible; bounds the claim)
if [[ "$GENERATED_CORPUS" -eq 1 ]]; then
echo "=== generating deterministic probe corpus ==="
mkdir -p "$CORPUS_DIR/texts"
for f in 0 1 2; do
    {
        printf '=== corpus text file idx=%s ===\n' "$f"
        awk 'BEGIN{ for (i = 0; i < 200; i++) print "The quick brown fox jumps over the lazy dog." }'
        printf '=== end idx=%s ===\n' "$f"
    } >"$CORPUS_DIR/texts/doc_$f.txt"
done
LINES_PER_FILE=4000
for d in 0 1; do
    mkdir -p "$CORPUS_DIR/dupe_$d"
    for f in 0 1 2; do
        awk -v n="$LINES_PER_FILE" -v idx="$f" \
            'BEGIN{ for (i = 0; i < n; i++) printf "duplicate payload %d\n", idx }' \
            >"$CORPUS_DIR/dupe_$d/dup_$f.dat"
    done
done
mkdir -p "$CORPUS_DIR/zeros"
dd if=/dev/zero of="$CORPUS_DIR/zeros/zero_0.dat" bs=1m count=2 2>/dev/null
dd if=/dev/zero of="$CORPUS_DIR/zeros/zero_1.dat" bs=1m count=2 2>/dev/null
mkdir -p "$CORPUS_DIR/rand"
for i in 0 1 2; do
    awk -v seed=$((42 + i * 7919)) 'BEGIN{
        s = seed
        for (j = 0; j < 300000; j++) { s = (16807 * s) % 2147483647; printf "%c", s % 256 }
    }' >"$CORPUS_DIR/rand/rand_$i.bin"
done
else
    echo "=== using supplied corpus read-only ==="
fi
CORPUS_FILES=$(find "$CORPUS_DIR" -type f | wc -l | tr -d ' ')
CORPUS_BYTES=$(find "$CORPUS_DIR" -type f -exec cat {} + | wc -c | tr -d ' ')
echo "files: $CORPUS_FILES"
echo "bytes: $CORPUS_BYTES ($(awk -v b="$CORPUS_BYTES" 'BEGIN{printf "%.1f MiB", b/1048576}'))"

# ---- measurement ------------------------------------------------------------
run_profile() { # profile dirname
    local profile="$1" dir="$2"
    local manifest_path="$WORK_DIR/$profile.corpus.manifest.json"
    echo ""
    echo "=== savings report: $profile ==="
    go run -tags=savingsreport "$RUNNER" -profile "$profile" \
        -repo "$WORK_DIR/$dir" -corpus "$CORPUS_DIR" \
        -manifest-out "$manifest_path" \
        -evidence-out "$WORK_DIR/$profile.candidate-evidence.json"
    if [[ -f "$WORK_DIR/corpus.manifest.json" ]] &&
        ! cmp -s "$WORK_DIR/corpus.manifest.json" "$manifest_path"; then
        echo "error: corpus manifest changed between repository profiles" >&2
        return 1
    fi
    cp "$manifest_path" "$WORK_DIR/corpus.manifest.json"
}

case "$PROFILE" in
    raw)  run_profile raw  raw-repo ;;
    zstd) run_profile zstd zstd-repo ;;
    both)
        run_profile raw  raw-repo
        run_profile zstd zstd-repo
        ;;
esac

mkdir -p "$WORK_DIR"
find "$CORPUS_DIR" -type f | sort >"$WORK_DIR/corpus-file-list.txt"
echo ""
echo "done: corpus file list at $WORK_DIR/corpus-file-list.txt"
if [[ "$KEEP" -ne 1 ]]; then
    echo "note: temp corpus and work dirs removed (use --keep to retain)"
else
    echo "retained: corpus=$CORPUS_DIR work=$WORK_DIR"
fi
exit 0
