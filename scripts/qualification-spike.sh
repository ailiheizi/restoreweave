#!/usr/bin/env bash
#=============================================================================
# qualification-spike.sh
#
# Reproducible backup-engine qualification spike: Kopia vs Restic vs Plakar.
#
# Generates a deterministic corpus (fixed seed; lorem text / duplicate /
# pseudo-random / all-zero / deeply nested content), then for each engine:
# create repository, backup, list snapshots, measure repo size, restore to an
# empty directory, run verify/check, and diff the restored tree against the
# corpus. Writes results.tsv and results.md into the work directory and
# prints a summary to stdout.
#
# Usage:
#   scripts/qualification-spike.sh --corpus-dir DIR --work-dir DIR [--size-mb N]
#
#   --corpus-dir DIR  where the deterministic corpus is generated (existing
#                     content is replaced)
#   --work-dir DIR    where repos, logs and results are written
#                     (results.tsv, results.md, corpus-file-list.txt)
#   --size-mb N       target corpus size in MB (default 150, matching the
#                     original spike corpus of 151.5 MB)
#
# Dependencies: bash, awk, dd, date, du, diff, find, grep, head, mkdir, rm,
#   seq, printf, wc. No python/jq/curl required.
#
# Engine binaries: $KOPIA_BIN / $RESTIC_BIN / $PLAKAR_BIN override the binary
#   paths; otherwise the engines are looked up in $PATH. An engine that is
#   missing or fails is recorded as a FAILED row and the remaining engines
#   still run; the script itself exits 0 as long as it completes.
#
# Notes:
#   - Repository password is fixed to "spike-test" (test-only data).
#   - All engine state (config/cache/log/repo) lives under --work-dir, so the
#     script leaves nothing behind outside the two DIRs.
#   - Timing uses `date +%s%N` where supported, seconds otherwise.
#   - Plakar v1.1.x: `create` is a top-level subcommand; do NOT pass
#     `-disable-security-check` to `create` (it exits 0 but creates nothing).
#   - Restic restores the source's absolute path under --target; the restored
#     corpus root is located with find before diffing.
#=============================================================================
set -euo pipefail

# ---- options ---------------------------------------------------------------
CORPUS_DIR=""
WORK_DIR=""
SIZE_MB=150

usage() {
    echo "usage: $0 --corpus-dir DIR --work-dir DIR [--size-mb N]" >&2
    echo "       (KOPIA_BIN / RESTIC_BIN / PLAKAR_BIN override engine paths)" >&2
    exit 2
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --corpus-dir) CORPUS_DIR="$2"; shift 2 ;;
        --work-dir)   WORK_DIR="$2";   shift 2 ;;
        --size-mb)    SIZE_MB="$2";    shift 2 ;;
        -h|--help)    usage ;;
        *) echo "unknown option: $1" >&2; usage ;;
    esac
done

[[ -n "$CORPUS_DIR" && -n "$WORK_DIR" ]] || usage
[[ "$SIZE_MB" =~ ^[0-9]+$ ]] || { echo "error: --size-mb must be an integer" >&2; exit 2; }

KOPIA_BIN="${KOPIA_BIN:-kopia}"
RESTIC_BIN="${RESTIC_BIN:-restic}"
PLAKAR_BIN="${PLAKAR_BIN:-plakar}"

# ---- helpers ---------------------------------------------------------------
abs_path() {
    case "$1" in /*) echo "$1" ;; *) echo "$(pwd)/$1" ;; esac
}
CORPUS_DIR=$(abs_path "$CORPUS_DIR")
WORK_DIR=$(abs_path "$WORK_DIR")

# nanosecond timing where possible, otherwise whole seconds
NS_PROBE=$(date +%N 2>/dev/null) || NS_PROBE="%N"
if [[ "$NS_PROBE" == "%N" || -z "$NS_PROBE" ]]; then
    NS_SCALE=1
    now_ns() { date +%s; }
else
    NS_SCALE=1000000
    now_ns() { date +%s%N; }
fi

fmt_sec() { awk -v ms="$1" 'BEGIN{printf "%.3f", ms/1000}'; }
ratio_of() { awk -v r="$1" -v c="$2" 'BEGIN{ if (c>0) printf "%.3f", r/c; else printf "n/a" }'; }

TSV="$WORK_DIR/results.tsv"
LOG_BASE="$WORK_DIR"

# locate the restored corpus root inside a restore target:
#   kopia/plakar restore the snapshot contents at the target root,
#   restic restores the full absolute source path below the target.
restored_corpus_dir() {
    local base r
    base=$(basename "$CORPUS_DIR")
    r=$(find "$1" -type d -name "$base" -print -quit 2>/dev/null) || true
    if [[ -n "$r" ]]; then printf '%s' "$r"; else printf '%s' "$1"; fi
}

write_fail_row() { # engine version step
    printf '%s\t%s\t%s\tFAILED\tFAILED\tFAILED\tFAILED\tFAILED\tFAILED\tFAILED\n' \
        "$1" "$2" "$CORPUS_BYTES" >>"$TSV"
    echo "[$1] FAILED at step '$3' (see $LOG_BASE/$1.log)" >&2
}

# ---- corpus generation (pure bash + awk + dd, deterministic) ---------------
generate_corpus() {
    local root="$CORPUS_DIR"
    local block_repeats dupe_lines rand_count zero_count
    read -r block_repeats dupe_lines rand_count zero_count <<<"$(awk -v m="$SIZE_MB" 'BEGIN{
        f = m / 150.0
        br = int(500*f + 0.5); if (br < 50)  br = 50
        dl = int(20000*f + 0.5); if (dl < 2000) dl = 2000
        rc = int(30*f + 0.5); if (rc < 3)   rc = 3
        zc = int(5*f + 0.5); if (zc < 1)    zc = 1
        printf "%d %d %d %d\n", br, dl, rc, zc
    }')"

    rm -rf "$root"
    mkdir -p "$root"

    # (a) 3 dirs x 20 text files: shared lorem block + per-file header/footer
    #     (dedup-friendly: all files share one ~0.5 MB content block)
    awk -v n="$block_repeats" 'BEGIN{
        line = "The quick brown fox jumps over the lazy dog. "
        for (k = 1; k < 24; k++) line = line "The quick brown fox jumps over the lazy dog. "
        for (i = 0; i < n; i++) print line
    }' >"$WORK_DIR/.lorem.tmp"
    for d in 0 1 2; do
        mkdir -p "$root/texts_a$d"
        for f in $(seq -w 0 19); do
            {
                printf '=== corpus text file dir=%s idx=%s ===\n' "$d" "$f"
                cat "$WORK_DIR/.lorem.tmp"
                printf '=== end dir=%s idx=%s ===\n' "$d" "$f"
            } >"$root/texts_a$d/doc_$f.txt"
        done
    done
    rm -f "$WORK_DIR/.lorem.tmp"

    # (b) two directories that are exact duplicates of each other (dedup target)
    for d in 0 1; do
        mkdir -p "$root/dupe_$d"
        for f in $(seq -w 0 9); do
            awk -v n="$dupe_lines" -v idx="$f" \
                'BEGIN{ for (i = 0; i < n; i++) printf "duplicate payload %d\n", idx }' \
                >"$root/dupe_$d/dup_$f.dat"
        done
    done

    # (c) seeded-LCG pseudo-random binaries, 1-3 MB, all different, incompressible
    mkdir -p "$root/rand_blobs"
    for i in $(seq 0 $((rand_count - 1))); do
        size=$(awk -v i="$i" 'BEGIN{
            printf "%d", 1000000 + ((i*7919 + 3) % 3)*1000000 + ((i*104729 + 17) % 1000000)
        }')
        awk -v seed=$((42 + i * 7919)) -v size="$size" 'BEGIN{
            s = seed
            while (size > 0) { s = (16807 * s) % 2147483647; printf "%c", s % 256; size-- }
        }' >"$root/rand_blobs/rand_$(printf '%02d' "$i").bin"
    done

    # (d) all-zero files (compress/dedup target)
    mkdir -p "$root/zeros"
    for i in $(seq 0 $((zero_count - 1))); do
        dd if=/dev/zero of="$root/zeros/zero_$(printf '%02d' "$i").dat" bs=1m count=10 2>/dev/null
    done

    # (e) deeply nested tree, depth 8, 3 leaves per level
    local path="$root/deep"
    for d in $(seq 0 7); do
        path="$path/level_$d"
        mkdir -p "$path"
        for j in 0 1 2; do
            awk -v d="$d" -v j="$j" \
                'BEGIN{ for (i = 0; i < 5000; i++) printf "leaf %d-%d content\n", d, j }' \
                >"$path/leaf_$j.txt"
        done
    done
}

corpus_stats() {
    find "$CORPUS_DIR" -type f | sort >"$WORK_DIR/corpus-file-list.txt"
    CORPUS_FILES=$(wc -l <"$WORK_DIR/corpus-file-list.txt" | tr -d ' ')
    CORPUS_BYTES=$(find "$CORPUS_DIR" -type f -exec cat {} + | wc -c | tr -d ' ')
    CORPUS_KIB=$(du -sk "$CORPUS_DIR" 2>/dev/null | awk '{print $1}') || CORPUS_KIB=0
    echo "--- corpus stats ---"
    echo "files: $CORPUS_FILES"
    echo "bytes: $CORPUS_BYTES ($(awk -v b="$CORPUS_BYTES" 'BEGIN{printf "%.1f MiB", b/1048576}'))"
    echo "du -sk: $CORPUS_KIB KiB"
    echo "--- corpus file list (also saved to $WORK_DIR/corpus-file-list.txt) ---"
    cat "$WORK_DIR/corpus-file-list.txt"
}

# ---- subcommand probing ----------------------------------------------------
# Plakar versions differ in their subcommand layout (v1.1.x uses top-level
# `create`; probe --help in case a future version renames it).
PLAKAR_CREATE_CMD=create
PLAKAR_CHECK_CMD=check
PLAKAR_PROBE=$("$PLAKAR_BIN" --help 2>/dev/null) || true
if printf '%s\n' "$PLAKAR_PROBE" | grep -qE '^  create '; then
    PLAKAR_CREATE_CMD=create
elif printf '%s\n' "$PLAKAR_PROBE" | grep -qE '^  repo '; then
    PLAKAR_CREATE_CMD=repo
fi
if printf '%s\n' "$PLAKAR_PROBE" | grep -qE '^  check '; then
    PLAKAR_CHECK_CMD=check
elif printf '%s\n' "$PLAKAR_PROBE" | grep -qE '^  verify '; then
    PLAKAR_CHECK_CMD=verify
fi
unset PLAKAR_PROBE

KOPIA_VERIFY_CMD=verify
if "$KOPIA_BIN" snapshot --help 2>/dev/null | grep -qE '(^|[[:space:]])verify([[:space:]]|$)'; then
    KOPIA_VERIFY_CMD=verify
fi

RESTIC_CHECK_CMD=check
if "$RESTIC_BIN" --help 2>/dev/null | grep -qE '^  check '; then
    RESTIC_CHECK_CMD=check
fi

# ---- engines ---------------------------------------------------------------
eng_kopia() {
    local e=kopia step ver tmp
    local repo="$WORK_DIR/kopia-repo" rdir="$WORK_DIR/kopia-restore"
    local log="$WORK_DIR/$e.log" list="$WORK_DIR/$e.list"
    : >"$log"

    step="binary check"
    ver="binary-not-found"
    if ! command -v "$KOPIA_BIN" >/dev/null 2>&1; then write_fail_row "$e" "$ver" "$step"; return 0; fi
    tmp=$("$KOPIA_BIN" --version 2>/dev/null | head -1) || true
    ver="${tmp:-unknown}"

    export KOPIA_CONFIG_PATH="$WORK_DIR/kopia.config"
    export KOPIA_CACHE_DIRECTORY="$WORK_DIR/kopia-cache"
    export KOPIA_LOG_DIR="$WORK_DIR/kopia-log"
    rm -f "$KOPIA_CONFIG_PATH"
    rm -rf "$repo" "$rdir" "$KOPIA_CACHE_DIRECTORY" "$KOPIA_LOG_DIR"
    mkdir -p "$rdir"

    step="repo create"
    if ! "$KOPIA_BIN" repository create filesystem --path "$repo" \
        --password spike-test --no-check-for-updates >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi

    step="backup"
    local b0 bms
    b0=$(now_ns)
    if ! "$KOPIA_BIN" snapshot create "$CORPUS_DIR" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    bms=$((($(now_ns) - b0) / NS_SCALE))

    step="snapshot list"
    if ! "$KOPIA_BIN" snapshot list >"$list" 2>>"$log"; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi

    step="repo size"
    local repo_kib
    repo_kib=$(du -sk "$repo" 2>>"$log" | awk '{print $1}') || repo_kib=0

    step="restore"
    local sid r0 rms
    sid=$(grep -oE 'k[0-9a-f]{32}' "$list" | head -1) || true
    if [[ -z "$sid" ]]; then write_fail_row "$e" "$ver" "$step (no snapshot id)"; return 0; fi
    r0=$(now_ns)
    if ! "$KOPIA_BIN" snapshot restore "$sid" "$rdir" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    rms=$((($(now_ns) - r0) / NS_SCALE))

    step="verify"
    local v0 vms vok=NO
    v0=$(now_ns)
    if "$KOPIA_BIN" snapshot "$KOPIA_VERIFY_CMD" >>"$log" 2>&1; then
        vok=YES
    fi
    vms=$((($(now_ns) - v0) / NS_SCALE))

    step="diff"
    local dok=NO rroot
    rroot=$(restored_corpus_dir "$rdir")
    if [[ -n "$rroot" ]] && diff -r "$CORPUS_DIR" "$rroot" >/dev/null 2>&1; then
        dok=YES
    fi

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$e" "$ver" "$CORPUS_BYTES" "$repo_kib" "$(ratio_of "$repo_kib" "$CORPUS_KIB")" \
        "$(fmt_sec "$bms")" "$(fmt_sec "$rms")" "$(fmt_sec "$vms")" "$vok" "$dok" >>"$TSV"
    echo "[$e] $ver: repo=${repo_kib}KiB backup=$(fmt_sec "$bms")s restore=$(fmt_sec "$rms")s verify=$(fmt_sec "$vms")s verify_ok=$vok diff_ok=$dok"
}

eng_restic() {
    local e=restic step ver tmp
    local repo="$WORK_DIR/restic-repo" rdir="$WORK_DIR/restic-restore"
    local log="$WORK_DIR/$e.log" list="$WORK_DIR/$e.list"
    : >"$log"

    step="binary check"
    ver="binary-not-found"
    if ! command -v "$RESTIC_BIN" >/dev/null 2>&1; then write_fail_row "$e" "$ver" "$step"; return 0; fi
    tmp=$("$RESTIC_BIN" version 2>/dev/null | head -1) || true
    ver="${tmp:-unknown}"

    export RESTIC_REPOSITORY="$repo"
    export RESTIC_PASSWORD=spike-test
    rm -rf "$repo" "$rdir"
    mkdir -p "$rdir"

    step="repo init"
    if ! "$RESTIC_BIN" init >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi

    step="backup"
    local b0 bms
    b0=$(now_ns)
    if ! "$RESTIC_BIN" backup "$CORPUS_DIR" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    bms=$((($(now_ns) - b0) / NS_SCALE))

    step="snapshot list"
    if ! "$RESTIC_BIN" snapshots >"$list" 2>>"$log"; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi

    step="repo size"
    local repo_kib
    repo_kib=$(du -sk "$repo" 2>>"$log" | awk '{print $1}') || repo_kib=0

    step="restore"
    local r0 rms
    r0=$(now_ns)
    if ! "$RESTIC_BIN" restore latest --target "$rdir" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    rms=$((($(now_ns) - r0) / NS_SCALE))

    step="verify"
    local v0 vms vok=NO
    v0=$(now_ns)
    if "$RESTIC_BIN" "$RESTIC_CHECK_CMD" >>"$log" 2>&1; then
        vok=YES
    fi
    vms=$((($(now_ns) - v0) / NS_SCALE))

    step="diff"
    local dok=NO rroot
    rroot=$(restored_corpus_dir "$rdir")
    if [[ -n "$rroot" ]] && diff -r "$CORPUS_DIR" "$rroot" >/dev/null 2>&1; then
        dok=YES
    fi

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$e" "$ver" "$CORPUS_BYTES" "$repo_kib" "$(ratio_of "$repo_kib" "$CORPUS_KIB")" \
        "$(fmt_sec "$bms")" "$(fmt_sec "$rms")" "$(fmt_sec "$vms")" "$vok" "$dok" >>"$TSV"
    echo "[$e] $ver: repo=${repo_kib}KiB backup=$(fmt_sec "$bms")s restore=$(fmt_sec "$rms")s verify=$(fmt_sec "$vms")s verify_ok=$vok diff_ok=$dok"
}

eng_plakar() {
    local e=plakar step ver tmp
    local repo="$WORK_DIR/plakar-repo" rdir="$WORK_DIR/plakar-restore"
    local log="$WORK_DIR/$e.log" list="$WORK_DIR/$e.list"
    : >"$log"

    step="binary check"
    ver="binary-not-found"
    if ! command -v "$PLAKAR_BIN" >/dev/null 2>&1; then write_fail_row "$e" "$ver" "$step"; return 0; fi
    tmp=$("$PLAKAR_BIN" version 2>/dev/null | head -1) || true
    ver="${tmp:-unknown}"

    export PLAKAR_REPOSITORY="fs://$repo"
    export PLAKAR_PASSPHRASE=spike-test
    local opts=(-configdir "$WORK_DIR/plakar-config" -cachedir "$WORK_DIR/plakar-cache" \
        -datadir "$WORK_DIR/plakar-data")
    rm -rf "$repo" "$rdir" "$WORK_DIR/plakar-config" "$WORK_DIR/plakar-cache" "$WORK_DIR/plakar-data"
    mkdir -p "$rdir"

    step="repo create"
    if [[ "$PLAKAR_CREATE_CMD" == repo ]]; then
        if ! "$PLAKAR_BIN" "${opts[@]}" repo create >>"$log" 2>&1; then
            write_fail_row "$e" "$ver" "$step"; return 0
        fi
    else
        if ! "$PLAKAR_BIN" "${opts[@]}" "$PLAKAR_CREATE_CMD" >>"$log" 2>&1; then
            write_fail_row "$e" "$ver" "$step"; return 0
        fi
    fi

    step="backup"
    local b0 bms
    b0=$(now_ns)
    if ! "$PLAKAR_BIN" "${opts[@]}" backup "$CORPUS_DIR" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    bms=$((($(now_ns) - b0) / NS_SCALE))

    step="snapshot list"
    if ! "$PLAKAR_BIN" "${opts[@]}" ls >"$list" 2>>"$log"; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi

    step="repo size"
    local repo_kib
    repo_kib=$(du -sk "$repo" 2>>"$log" | awk '{print $1}') || repo_kib=0

    step="restore"
    local r0 rms
    r0=$(now_ns)
    if ! "$PLAKAR_BIN" "${opts[@]}" restore -to "$rdir" >>"$log" 2>&1; then
        write_fail_row "$e" "$ver" "$step"; return 0
    fi
    rms=$((($(now_ns) - r0) / NS_SCALE))

    step="verify"
    local v0 vms vok=NO
    v0=$(now_ns)
    if "$PLAKAR_BIN" "${opts[@]}" "$PLAKAR_CHECK_CMD" >>"$log" 2>&1; then
        vok=YES
    fi
    vms=$((($(now_ns) - v0) / NS_SCALE))

    step="diff"
    local dok=NO rroot
    rroot=$(restored_corpus_dir "$rdir")
    if [[ -n "$rroot" ]] && diff -r "$CORPUS_DIR" "$rroot" >/dev/null 2>&1; then
        dok=YES
    fi

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$e" "$ver" "$CORPUS_BYTES" "$repo_kib" "$(ratio_of "$repo_kib" "$CORPUS_KIB")" \
        "$(fmt_sec "$bms")" "$(fmt_sec "$rms")" "$(fmt_sec "$vms")" "$vok" "$dok" >>"$TSV"
    echo "[$e] $ver: repo=${repo_kib}KiB backup=$(fmt_sec "$bms")s restore=$(fmt_sec "$rms")s verify=$(fmt_sec "$vms")s verify_ok=$vok diff_ok=$dok"
}

# ---- results rendering -----------------------------------------------------
render_md() {
    {
        echo "# Repository Engine Qualification Spike — scripted re-run"
        echo ""
        echo "Generated by scripts/qualification-spike.sh on $(LC_ALL=C date '+%Y-%m-%d %H:%M:%S %Z')."
        echo "Corpus: $CORPUS_DIR (target ${SIZE_MB} MB, $CORPUS_FILES files, $CORPUS_BYTES bytes). Raw values in results.tsv."
        echo ""
        awk -F '\t' '
            BEGIN {
                print "| Engine | Version | Corpus bytes | Repo KiB | Ratio | Backup s | Restore s | Verify s | Verify OK | Diff OK |"
                print "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |"
            }
            NR > 1 {
                printf "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
                    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
            }
        ' "$TSV"
    } >"$WORK_DIR/results.md"
}

# ---- main ------------------------------------------------------------------
mkdir -p "$WORK_DIR"
printf 'engine\tversion\tcorpus_bytes\trepo_kib\tratio\tbackup_sec\trestore_sec\tverify_sec\tverify_ok\tdiff_ok\n' >"$TSV"

echo "=== corpus generation ==="
generate_corpus
corpus_stats

echo ""
echo "=== engine runs ==="
eng_kopia
eng_restic
eng_plakar

echo ""
echo "=== results (results.tsv) ==="
cat "$TSV"
render_md

echo ""
echo "done: results.tsv / results.md / corpus-file-list.txt written to $WORK_DIR"
exit 0
