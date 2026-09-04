#!/usr/bin/env bash
# Candidate-only offline artifact assembly. This does not establish a
# supported or release-qualified platform package.
set -euo pipefail
export LC_ALL=C
export TZ=UTC
umask 077

usage() {
  echo "usage: $0 --version VERSION --os OS --arch ARCH --rw FILE --daemon FILE --web-dist DIR --semantic-archive FILE --output FILE" >&2
}

version=""; target_os=""; target_arch=""; rw_bin=""; daemon_bin=""; web_dist=""; semantic_archive=""; output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="${2:-}"; shift 2;;
    --os) target_os="${2:-}"; shift 2;;
    --arch) target_arch="${2:-}"; shift 2;;
    --rw) rw_bin="${2:-}"; shift 2;;
    --daemon) daemon_bin="${2:-}"; shift 2;;
    --web-dist) web_dist="${2:-}"; shift 2;;
    --semantic-archive) semantic_archive="${2:-}"; shift 2;;
    --output) output="${2:-}"; shift 2;;
    -h|--help) usage; exit 0;;
    *) usage; exit 2;;
  esac
done

[[ -n "$version" && -n "$target_os" && -n "$target_arch" && -n "$rw_bin" && -n "$daemon_bin" && -n "$web_dist" && -n "$semantic_archive" && -n "$output" ]] || { usage; exit 2; }
[[ "$version" =~ ^[A-Za-z0-9._+-]+$ && "$target_os" =~ ^[A-Za-z0-9._+-]+$ && "$target_arch" =~ ^[A-Za-z0-9._+-]+$ ]] || { echo "error: unsafe version or platform" >&2; exit 2; }

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
canonical_file() {
  local path="$1" parent base
  [[ "$path" = /* ]] || { echo "error: input must be absolute: $path" >&2; return 1; }
  parent="$(dirname "$path")"; base="$(basename "$path")"
  [[ -d "$parent" ]] || { echo "error: input parent is unavailable: $path" >&2; return 1; }
  parent="$(cd "$parent" && pwd -P)"
  printf '%s/%s\n' "$parent" "$base"
}
regular_file() {
  local path="$1"
  [[ -f "$path" && ! -L "$path" ]] || { echo "error: expected regular non-symlink file: $path" >&2; exit 2; }
}
executable_file() {
  regular_file "$1"
  [[ -x "$1" ]] || { echo "error: expected executable file: $1" >&2; exit 2; }
}
regular_dir() {
  local path="$1"
  [[ -d "$path" && ! -L "$path" ]] || { echo "error: expected regular non-symlink directory: $path" >&2; exit 2; }
  local item rel
  while IFS= read -r -d '' item; do
    rel="${item#"$path/"}"
    [[ "$rel" != *$'\n'* ]] || { echo "error: web asset relative path contains newline: $rel" >&2; exit 2; }
    [[ ! -L "$item" ]] || { echo "error: web assets must not contain symlinks: $item" >&2; exit 2; }
    [[ -f "$item" || -d "$item" ]] || { echo "error: web assets contain a non-regular entry: $item" >&2; exit 2; }
  done < <(find "$path" -print0)
}
canonical_dir() {
  local path="$1"
  [[ "$path" = /* ]] || { echo "error: directory input must be absolute: $path" >&2; return 1; }
  [[ -d "$path" && ! -L "$path" ]] || { echo "error: expected regular non-symlink directory: $path" >&2; return 1; }
  (cd "$path" && pwd -P)
}

rw_bin="$(canonical_file "$rw_bin")"; daemon_bin="$(canonical_file "$daemon_bin")"; semantic_archive="$(canonical_file "$semantic_archive")"; web_dist="$(canonical_dir "$web_dist")"
executable_file "$rw_bin"; executable_file "$daemon_bin"; regular_file "$semantic_archive"; regular_dir "$web_dist"
[[ "$semantic_archive" == *.tar.gz || "$semantic_archive" == *.tgz ]] || { echo "error: semantic archive must be gzip tar (.tar.gz or .tgz)" >&2; exit 2; }
[[ -f "$web_dist/index.html" && ! -L "$web_dist/index.html" ]] || { echo "error: web-dist/index.html must be a regular non-symlink file" >&2; exit 2; }
[[ -f "$script_root/LICENSE" && ! -L "$script_root/LICENSE" ]] || { echo "error: project LICENSE is unavailable" >&2; exit 2; }
[[ "$output" == *.tar.gz ]] || { echo "error: output must end in .tar.gz" >&2; exit 2; }
output="$(canonical_file "$(dirname "$output")/$(basename "$output")")"
[[ ! -e "$output" && ! -L "$output" ]] || { echo "error: output already exists" >&2; exit 2; }

hash_cmd=""
if command -v sha256sum >/dev/null 2>&1; then hash_cmd=sha256sum; elif command -v shasum >/dev/null 2>&1; then hash_cmd=shasum; else echo "error: sha256sum or shasum is required" >&2; exit 1; fi
tar_cmd="$(command -v tar || true)"; [[ -n "$tar_cmd" ]] || { echo "error: tar is required" >&2; exit 1; }
gzip_cmd="$(command -v gzip || true)"; [[ -n "$gzip_cmd" ]] || { echo "error: gzip is required" >&2; exit 1; }
hash_file() {
  local path="$1" line digest
  if [[ "$hash_cmd" == "sha256sum" ]]; then
    line="$(sha256sum "$path")" || return 1
  else
    line="$(shasum -a 256 "$path")" || return 1
  fi
  digest="${line%% *}"
  [[ "$digest" =~ ^[0-9a-fA-F]{64}$ ]] || return 1
  printf '%s' "$digest"
}

# The wrapper must carry the real in-process zvec backend. A daemon built
# without the purego tag compiles the unavailable placeholder, even when a
# semantic archive is placed beside it. Go BuildInfo provides an auditable
# admission check for the build tag, pinned dependency, and target platform.
go_cmd="$(command -v go || true)"
[[ -n "$go_cmd" ]] || { echo "error: Go is required to inspect daemon BuildInfo" >&2; exit 2; }
daemon_build_info="$($go_cmd version -m "$daemon_bin" 2>/dev/null)" || { echo "error: daemon has no readable Go BuildInfo" >&2; exit 2; }
daemon_main_path="$(printf '%s\n' "$daemon_build_info" | awk '$1 == "path" { print $2; exit }')"
[[ "$daemon_main_path" == "github.com/ailiheizi/restoreweave/server/cmd/restoreweaved" ]] || { echo "error: daemon BuildInfo main path is not restoreweaved" >&2; exit 2; }
daemon_tags="$(printf '%s\n' "$daemon_build_info" | awk -F= '$1 ~ /\tbuild\t-tags/ { print $2; exit }')"
[[ "$daemon_tags" == "purego" ]] || { echo "error: daemon must be built with exactly -tags=purego (in-process zvec)" >&2; exit 2; }
zvec_module="github.com/zvec-ai/zvec-go"
zvec_version="$(printf '%s\n' "$daemon_build_info" | awk -v module="$zvec_module" '$1 == "dep" && $2 == module { print $3; exit }')"
zvec_sum="$(printf '%s\n' "$daemon_build_info" | awk -v module="$zvec_module" '$1 == "dep" && $2 == module { print $4; exit }')"
zvec_expected_version="v0.6.1-0.20260721023313-9199195b29da"
zvec_expected_sum="h1:4wINeawyVOYz/Rj4mDJQlSAUYLkQ76QELU1dd2IEU3k="
[[ "$zvec_version" == "$zvec_expected_version" ]] || { echo "error: daemon zvec-go pin $zvec_version does not match $zvec_expected_version" >&2; exit 2; }
[[ "$zvec_sum" == "$zvec_expected_sum" ]] || { echo "error: daemon zvec-go sum does not match the pinned module" >&2; exit 2; }
zvec_commit="${zvec_version##*-}"
daemon_go_version="$(printf '%s\n' "$daemon_build_info" | sed -n '1s/.*: //p')"
[[ "$daemon_go_version" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?([a-z0-9._+-]*)$ ]] || { echo "error: daemon Go version is not a safe token" >&2; exit 2; }
daemon_goos="$(printf '%s\n' "$daemon_build_info" | awk -F= '$1 ~ /\tbuild\tGOOS/ { print $2; exit }')"
daemon_goarch="$(printf '%s\n' "$daemon_build_info" | awk -F= '$1 ~ /\tbuild\tGOARCH/ { print $2; exit }')"
[[ "$daemon_goos" =~ ^[A-Za-z0-9._+-]+$ && "$daemon_goarch" =~ ^[A-Za-z0-9._+-]+$ ]] || { echo "error: daemon platform metadata is not a safe token" >&2; exit 2; }
[[ "$daemon_goos" == "$target_os" && "$daemon_goarch" == "$target_arch" ]] || {
  echo "error: daemon platform ${daemon_goos}/${daemon_goarch} does not match requested ${target_os}/${target_arch}" >&2
  exit 2
}

package_name="restoreweave-${version}-${target_os}-${target_arch}"
stage="$(mktemp -d "${TMPDIR:-/tmp}/restoreweave-package.XXXXXX")"
cleanup() { rm -rf "$stage"; }
trap cleanup EXIT
root="$stage/$package_name"; mkdir -p "$root/bin" "$root/web/dist" "$root/semantic" "$root/licenses"
cp "$rw_bin" "$root/bin/rw"; cp "$daemon_bin" "$root/bin/restoreweaved"
cp -R "$web_dist"/. "$root/web/dist/"
cp "$semantic_archive" "$root/semantic/semantic-bundle.tar.gz"

# Read evidence members without extracting untrusted archive paths. The
# semantic installer remains the authority for full bundle admission.
"$tar_cmd" -xOf "$semantic_archive" LICENSE >"$root/licenses/semantic-bundle.LICENSE"
"$tar_cmd" -xOf "$semantic_archive" NOTICE >"$root/licenses/semantic-bundle.NOTICE"
"$tar_cmd" -xOf "$semantic_archive" sbom.json >"$root/licenses/semantic-bundle.sbom.json"
cp "$script_root/LICENSE" "$root/LICENSE"
cp "$root/licenses/semantic-bundle.NOTICE" "$root/NOTICE"
cat >"$root/SBOM.json" <<EOF
{
  "schema": "restoreweave.candidate-artifact-sbom.v1",
  "status": "INCOMPLETE_NOT_RELEASE_SBOM",
  "note": "Candidate wrapper; it is not a complete release dependency inventory.",
  "semantic_bundle_sbom": "licenses/semantic-bundle.sbom.json"
}
EOF

archive_digest="$(hash_file "$root/semantic/semantic-bundle.tar.gz")" || { echo "error: hashing semantic archive failed" >&2; exit 1; }
cat >"$root/manifest.json" <<EOF
{
  "schema": "restoreweave.candidate-offline-artifact.v1",
  "status": "CANDIDATE_ONLY_NOT_SUPPORTED",
  "version": "${version}",
  "platform": {"os": "${target_os}", "arch": "${target_arch}"},
  "daemon_build": {"main_path": "${daemon_main_path}", "go_version": "${daemon_go_version}", "goos": "${daemon_goos}", "goarch": "${daemon_goarch}", "build_tags": "${daemon_tags}", "zvec_go": {"module": "${zvec_module}", "version": "${zvec_version}", "sum": "${zvec_sum}", "commit": "${zvec_commit}"}},
  "semantic_archive_sha256": "sha256:${archive_digest}",
  "layout": {"daemon": "bin/restoreweaved", "cli": "bin/rw", "web": "web/dist", "semantic": "semantic/semantic-bundle.tar.gz"}
}
EOF

find "$root" -type f -print | LC_ALL=C sort | while IFS= read -r file; do
  rel="${file#"$root/"}"
  if [[ "$rel" != "checksums.sha256" ]]; then
    digest="$(hash_file "$file")" || { echo "error: hashing $rel failed" >&2; exit 1; }
    printf '%s  %s\n' "$digest" "$rel"
  fi
done >"$root/checksums.sha256"
# Stable input bytes and fixed metadata make repeated assembly deterministic on
# the same host; cross-platform qualification remains an external gate.
find "$root" -exec touch -t 197001010000 {} +
mkdir -p "$(dirname "$output")"
tar_stage="$stage/package.tar"
COPYFILE_DISABLE=1 "$tar_cmd" -cf "$tar_stage" -C "$stage" "$package_name"
output_tmp="$(mktemp "$(dirname "$output")/.restoreweave-offline.XXXXXX")"
trap 'rm -rf "$stage" "$output_tmp"' EXIT
gzip -n -c "$tar_stage" >"$output_tmp"
sync
if ! ln "$output_tmp" "$output"; then
  echo "error: output appeared while packaging; refusing to replace it" >&2
  exit 2
fi
rm -f "$output_tmp"
echo "candidate artifact: $output"
artifact_digest="$(hash_file "$output")" || { echo "error: hashing candidate artifact failed" >&2; exit 1; }
echo "sha256: $artifact_digest"
